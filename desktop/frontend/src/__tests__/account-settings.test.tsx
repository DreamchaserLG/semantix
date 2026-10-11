import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import type { AccountRegistrationOptions, AccountView } from "../lib/types";

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', { url: "http://localhost/", pretendToBeVisual: true });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage, HTMLElement: dom.window.HTMLElement, HTMLInputElement: dom.window.HTMLInputElement, Event: dom.window.Event, IS_REACT_ACT_ENVIRONMENT: true });
Object.defineProperty(globalThis, "navigator", { value: dom.window.navigator, configurable: true });
const React = await import("react");
const { act } = React;
const { createRoot } = await import("react-dom/client");
const { AccountSettingsPage, formatCredits } = await import("../components/AccountSettingsPage");
const { LocaleProvider } = await import("../lib/i18n");
const signedOut: AccountView = { state: "signed_out", siteURL: "https://sem.ensureok.ai", stale: false, remembered: false, hasSession: false, warningCodes: [] };
const signedIn: AccountView = { ...signedOut, state: "signed_in", hasSession: true, remembered: true, syncedAt: "2026-10-11T12:00:00Z", user: { id: "7", username: "tester", displayName: "Test User", group: "default", credits: "1801439850948.1986", usedCredits: "0.0024", billingTokens: "9007199254740993", requestCount: "4" } };
let next = signedOut;
let loginArgs: string[] = [];
let verifyArgs: string[] = [];
let resolveLogin: ((view: AccountView) => void) | undefined;
let statusCalls = 0;
let registrationArgs: unknown[] = [];
let registrationCalls = 0;
let registrationOptions: AccountRegistrationOptions = { enabled: true, emailVerification: false, userAgreement: false, privacyPolicy: false, websiteRequired: false };
(window as any).go = { main: { App: {
  AccountStatus: async () => { statusCalls++; return next; },
  AccountLogin: (username: string, password: string) => { loginArgs = [username, password]; return new Promise<AccountView>((resolve) => { resolveLogin = resolve; }); },
  AccountVerify: async (code: string) => { verifyArgs = [code]; return signedIn; },
  AccountLogout: async () => { next = signedOut; return signedOut; },
  AccountRegistrationOptions: async () => registrationOptions,
  AccountRegister: async (...args: unknown[]) => { registrationCalls++; registrationArgs = args; return { success: true }; },
  AccountSendRegistrationCode: async () => ({ success: true }),
  DesktopStartupSettings: async () => ({ desktopLanguage: "en", desktopTheme: "light", desktopThemeStyle: "graphite" }),
} } };
const rootEl = document.getElementById("root")!;
const root = createRoot(rootEl);
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));
await act(async () => { root.render(React.createElement(LocaleProvider, null, React.createElement(AccountSettingsPage))); await flush(); });
assert.equal(statusCalls, 1);

async function fill(name: string, value: string) {
  const input = rootEl.querySelector<HTMLInputElement>(`input[name="${name}"]`)!;
  assert.ok(input, `input ${name} exists`);
  await act(async () => {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")!.set!.call(input, value);
    input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  });
}
async function submit() {
  await act(async () => { rootEl.querySelector("form")!.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true })); await flush(); });
}
async function click(text: string) {
  const button = [...rootEl.querySelectorAll("button")].find((b) => b.textContent === text);
  assert.ok(button, `button ${text} exists`);
  await act(async () => { button.click(); await flush(); });
}

await fill("username", " tester ");
await fill("password", " secret-password ");
await act(async () => rootEl.querySelector<HTMLButtonElement>('[aria-label="Show password"]')!.click());
assert.equal(rootEl.querySelector<HTMLInputElement>('input[name="password"]')!.type, "text");
await submit();
assert.deepEqual(loginArgs, ["tester", " secret-password "]);
assert.equal(rootEl.querySelector<HTMLInputElement>('input[name="password"]')!.value, "", "password cleared before request completes");
assert.equal(rootEl.querySelector<HTMLInputElement>('input[name="password"]')!.type, "password", "password visibility resets after submit");
assert.equal(rootEl.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled, true);
await act(async () => { resolveLogin!({ ...signedOut, errorCode: "invalid_credentials" }); await flush(); });
assert.match(rootEl.querySelector('[role="alert"]')!.textContent!, /Sign-in failed/);
assert.equal(localStorage.length, 0, "no account secret in browser storage");

await fill("password", "second-password");
await submit();
await act(async () => { resolveLogin!({ ...signedOut, state: "verifying" }); await flush(); });
assert.ok(rootEl.querySelector('input[name="code"]'));
assert.equal(rootEl.querySelector('input[name="password"]'), null);
assert.doesNotMatch(rootEl.textContent!, /Available credits/);
await fill("code", "123456");
await submit();
assert.deepEqual(verifyArgs, ["123456"]);
assert.doesNotMatch(rootEl.textContent!, /9,007,199,254,740,993/);
assert.match(rootEl.textContent!, /1,801,439,850,948\.1986/);
assert.match(rootEl.textContent!, /0\.0024/);
assert.equal(rootEl.querySelector("form"), null);
assert.equal(formatCredits("-42"), "-42");
assert.equal(formatCredits("0"), "0");
assert.equal(formatCredits("NaN"), "—");
assert.equal(formatCredits("-0.0002"), "-0.0002");
assert.equal(formatCredits("9007199254740993.1234"), "9,007,199,254,740,993.1234");
assert.equal(formatCredits("1.12345"), "—");

next = { ...signedIn, stale: true, errorCode: "network" };
await click("Refresh account");
assert.match(rootEl.textContent!, /last successful reading/);
assert.doesNotMatch(rootEl.textContent!, /9,007,199,254,740,993/);
next = { ...signedOut, errorCode: "session_expired" };
await click("Refresh account");
assert.doesNotMatch(rootEl.textContent!, /9,007,199,254,740,993/);
assert.ok(rootEl.querySelector('input[name="password"]'));

assert.doesNotMatch(rootEl.textContent!, /5,000|billing tokens|administrator allocates/);
await click("Create account");
assert.ok(rootEl.querySelector('input[name="confirmation"]'));
await fill("username", "new-user");
await fill("password", "password1");
await fill("confirmation", "different");
await submit();
assert.equal(registrationCalls, 0, "mismatched passwords never leave the form");
assert.match(rootEl.textContent!, /Passwords do not match/);
await fill("confirmation", "password1");
await submit();
assert.deepEqual(registrationArgs, ["new-user", "password1", "", "", false]);
assert.match(rootEl.textContent!, /Account created/);
assert.equal(rootEl.querySelector<HTMLInputElement>('input[name="password"]')!.value, "");
assert.equal(rootEl.querySelector('input[name="confirmation"]'), null);
assert.equal(localStorage.length, 0);
registrationOptions = { ...registrationOptions, emailVerification: true, userAgreement: true };
await click("Create account");
assert.ok(rootEl.querySelector('input[name="email"]'));
assert.ok(rootEl.querySelector('input[name="agreed"]'));
await fill("email", "user@example.com");
await click("Send code");
assert.match(rootEl.textContent!, /Code sent/);
assert.ok([...rootEl.querySelectorAll("button")].some((b) => b.textContent === "60s" && b.disabled));
await click("Sign in");
registrationOptions = { ...registrationOptions, websiteRequired: true };
await click("Create account");
assert.equal(rootEl.querySelector('input[name="password"]'), null, "challenge cannot be bypassed");
assert.match(rootEl.textContent!, /Complete verification on the account website/);
await click("Sign in");
registrationOptions = { ...registrationOptions, websiteRequired: false, emailVerification: false, userAgreement: false };
(window as any).go.main.App.AccountRegister = async () => { throw new Error("transport failed after dispatch"); };
await click("Create account");
await fill("password", "password1");
await fill("confirmation", "password1");
await submit();
assert.match(rootEl.textContent!, /Could not confirm registration/);
assert.equal(rootEl.querySelector<HTMLInputElement>('input[name="password"]')!.value, "");
await click("Sign in");

next = { ...signedOut, warningCodes: ["preview"] };
await click("Refresh account");
assert.equal(rootEl.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled, true, "preview cannot fake a login");
assert.match(rootEl.textContent!, /Browser preview only/);
await act(async () => root.unmount());
const { AccountEntry } = await import("../components/AccountEntry");
const entryRoot = createRoot(rootEl);
const bindings = (window as any).go.main.App;
let resolveStatus: ((value: AccountView) => void) | undefined;
bindings.AccountStatus = () => new Promise<AccountView>((resolve) => { resolveStatus = resolve; });
const workspace = React.createElement("div", { "data-workspace": true }, "Workspace", React.createElement(AccountSettingsPage));
await act(async () => { entryRoot.render(React.createElement(LocaleProvider, null, React.createElement(AccountEntry, { children: workspace }))); await new Promise((resolve) => setTimeout(resolve, 50)); });
assert.equal(rootEl.querySelector("[data-workspace]"), null, "workspace must not mount before validation");
await act(async () => { await new Promise((resolve) => setTimeout(resolve, 50)); });
assert.ok(resolveStatus, "startup requests account status");
await act(async () => { resolveStatus!(signedOut); await flush(); });
assert.ok(rootEl.querySelector('input[name="password"]'), "normal launch shows sign-in without a flag");
await fill("username", "tester");
await fill("password", "password1");
await submit();
bindings.AccountStatus = async () => next;
next = signedIn;
await act(async () => { resolveLogin!(signedIn); await flush(); });
assert.ok(rootEl.querySelector("[data-workspace]"), "successful login mounts workspace");
await click("Sign out / clear session");
assert.equal(rootEl.querySelector("[data-workspace]"), null, "sign-out unmounts workspace");
assert.ok(rootEl.querySelector('input[name="password"]'));
await act(async () => entryRoot.unmount());
next = signedIn;
const restoredRoot = createRoot(rootEl);
await act(async () => { restoredRoot.render(React.createElement(LocaleProvider, null, React.createElement(AccountEntry, { children: workspace }))); await flush(); });
assert.ok(rootEl.querySelector("[data-workspace]"), "verified saved session enters automatically");
await act(async () => restoredRoot.unmount());
dom.window.close();
console.log("Account UI: credentials, 2FA, exact credits, registration, startup restore, workspace mount/sign-out and preview passed.");
