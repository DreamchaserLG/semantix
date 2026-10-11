import { useCallback, useContext, useEffect, useRef, useState, type FormEvent } from "react";
import { ArrowUpRight, Eye, EyeOff, KeyRound, ShieldCheck, UserRound } from "lucide-react";
import { app, openExternal } from "../lib/bridge";
import { AccountSessionContext } from "../lib/accountSession";
import { useT, type DictKey } from "../lib/i18n";
import type { AccountRegistrationOptions, AccountRegistrationResult, AccountView } from "../lib/types";

const messages: Record<string, DictKey> = {
  invalid_input: "account.error.input", invalid_credentials: "account.error.credentials",
  invalid_code: "account.error.code", session_expired: "account.error.expired",
  network: "account.error.network", unavailable: "account.error.network",
  protocol: "account.error.protocol", rejected: "account.error.rejected",
  rate_limited: "account.error.rate", unsupported_verification: "account.error.verification",
  storage_unavailable: "account.warning.storage", storage_delete: "account.warning.delete",
  remote_logout: "account.warning.logout", logout_first: "account.error.logoutFirst",
  registration_disabled: "account.registerDisabled", registration_website: "account.registerWebsite",
  registration_input: "account.registerInput", registration_agreement: "account.agreementRequired",
  registration_unknown: "account.registerUnknown", registration_rejected: "account.registerRejected",
  preview: "account.preview",
};

export function formatCredits(value: string): string {
  if (!/^-?\d+(\.\d{1,4})?$/.test(value)) return "—";
  const [integer, fraction] = value.split(".");
  return integer.replace(/\B(?=(\d{3})+(?!\d))/g, ",") + (fraction ? `.${fraction}` : "");
}

export function AccountSettingsPage({ startup = false }: { startup?: boolean }) {
  const t = useT();
  const publish = useContext(AccountSessionContext);
  const [view, setView] = useState<AccountView | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState<DictKey | null>(null);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [code, setCode] = useState("");
  const [registering, setRegistering] = useState(false);
  const [options, setOptions] = useState<AccountRegistrationOptions | null>(null);
  const [email, setEmail] = useState("");
  const [agreed, setAgreed] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const active = useRef(false);
  const request = useRef(0);

  const perform = useCallback(async <T,>(operation: () => Promise<T>, accept: (result: T) => void, failureCode = "network") => {
    if (active.current) return;
    active.current = true;
    const id = ++request.current;
    setBusy(true); setError(""); setNotice(null);
    try {
      const next = await operation();
      if (id === request.current) accept(next);
    } catch {
      if (id === request.current) {
        setError(failureCode);
        setView((previous) => previous ? { ...previous, stale: true } : previous);
      }
    } finally {
      if (id === request.current) { active.current = false; setBusy(false); }
    }
  }, []);
  const run = useCallback((operation: () => Promise<AccountView>) => perform(operation, (next) => {
    setView(next); setError(next.errorCode ?? ""); publish?.(next);
  }), [perform, publish]);

  useEffect(() => {
    active.current = false;
    void run(() => app.AccountStatus());
    return () => { request.current += 1; active.current = false; };
  }, [run]);
  useEffect(() => {
    if (cooldown <= 0) return;
    const timer = window.setTimeout(() => setCooldown((seconds) => seconds - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [cooldown]);

  const clearSecrets = () => { setPassword(""); setConfirmation(""); setCode(""); setShowPassword(false); };
  const switchMode = () => {
    if (active.current) return;
    clearSecrets(); setError(""); setNotice(null); setAgreed(false);
    setRegistering(!registering);
    if (!registering) {
      setOptions(null);
      void perform(() => app.AccountRegistrationOptions(), (next) => { setOptions(next); setError(next.errorCode ?? ""); });
    }
  };
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (active.current) return;
    if (registering) {
      if (password !== confirmation) { setError("password_mismatch"); return; }
      // Keep all secrets in this dispatch's closure, then clear the form immediately.
      const args = [username.trim(), password, email.trim(), code.trim(), agreed] as const;
      void perform(() => app.AccountRegister(...args), (result: AccountRegistrationResult) => {
        if (result.success) { setRegistering(false); setNotice("account.registered"); }
        else setError(result.errorCode ?? "rejected");
      }, "registration_unknown");
    } else {
      const operation = view?.state === "verifying" ? () => app.AccountVerify(code) : () => app.AccountLogin(username.trim(), password);
      void run(operation);
    }
    clearSecrets();
  };
  const message = (key: string) => t(key === "password_mismatch" ? "account.passwordMismatch" : messages[key] ?? "account.error.rejected");
  const verifying = view?.state === "verifying";
  const preview = view?.warningCodes.includes("preview");
  const user = view?.state === "signed_in" ? view.user : undefined;
  const canRegister = options?.enabled && !options.websiteRequired && !options.errorCode;

  return <section className="account-settings" aria-busy={busy}>
    {!startup && <div className="account-settings__service">
      <span><ShieldCheck size={15} aria-hidden="true" />Semantix</span>
      <button type="button" className="btn btn--small" onClick={() => openExternal("https://sem.ensureok.ai")}>{t("account.console")}<ArrowUpRight size={14} aria-hidden="true" /></button>
    </div>}
    <div className="account-settings__notices">
      {error && <div className="banner banner--error" role="alert">{message(error)}</div>}
      {notice && <p className="account-settings__success" role="status">{t(notice)}</p>}
      {view?.warningCodes.map((warning) => <div key={warning} className="banner banner--warning" role="status">{message(warning)}</div>)}
      {busy && <div role="status" className="settings-section__desc">{t("account.loading")}</div>}
      {view?.stale && <p role="status" className="settings-section__desc">{t("account.stale")}</p>}
    </div>
    {user ? <div className="account-settings__panel">
      <div className="account-settings__identity"><div className="account-settings__avatar"><UserRound size={24} aria-hidden="true" /></div><div>
        <h3>{user.displayName || user.username}</h3><p>@{user.username}</p>
      </div><span className="account-settings__badge">{t("account.signedIn")}</span></div>
      <dl className="account-settings__credits">
        <div><dt>{t("account.remaining")}</dt><dd>{formatCredits(user.credits)}</dd></div>
        <div><dt>{t("account.used")}</dt><dd>{formatCredits(user.usedCredits)}</dd></div>
        <div><dt>{t("account.requests")}</dt><dd>{formatCredits(user.requestCount)}</dd></div>
      </dl>
      {/^(0(?:\.0+)?|-\d+(?:\.\d+)?)$/.test(user.credits) && <p className="banner banner--warning">{t("account.empty")}</p>}
      {view?.syncedAt && <p className="settings-section__desc">{t("account.synced", { time: new Date(view.syncedAt).toLocaleString() })}</p>}
    </div> : view && !view.hasSession && !view.warningCodes.includes("storage_delete") ? <div className="account-settings__login">
      <form onSubmit={submit} className="account-settings__form">
        <div className="account-settings__intro">
          {!startup && <div className="account-settings__avatar">{verifying ? <KeyRound size={25} aria-hidden="true" /> : <UserRound size={25} aria-hidden="true" />}</div>}
          <h3>{t(verifying ? "account.verifyTitle" : registering ? "account.registerTitle" : "account.welcome")}</h3>
          <p>{t(verifying ? "account.verifyHint" : registering ? "account.registerHint" : "account.welcomeHint")}</p>
        </div>
        {registering && options && !options.errorCode && (!options.enabled || options.websiteRequired) && <p role="status" className="settings-section__desc">{t(options.enabled ? "account.registerWebsite" : "account.registerDisabled")}</p>}
        {verifying ? <label className="account-settings__label">{t("account.code")}
          <input className="mem-input" name="code" autoComplete="one-time-code" spellCheck={false} value={code} maxLength={128} required disabled={busy} onChange={(event) => setCode(event.target.value)} />
        </label> : (!registering || canRegister) && <>
          <label className="account-settings__label">{t("account.username")}
            <input className="mem-input" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} placeholder={t(registering ? "account.registerUsername" : "account.usernamePlaceholder")} value={username} maxLength={registering ? 40 : 128} required disabled={busy || preview} onChange={(event) => setUsername(event.target.value)} />
          </label>
          <label className="account-settings__label">{t("account.password")}
            <span className="account-settings__password">
              <input className="mem-input" name="password" type={showPassword ? "text" : "password"} autoComplete={registering ? "new-password" : "current-password"} placeholder={t(registering ? "account.registerPassword" : "account.passwordPlaceholder")} value={password} maxLength={registering ? 40 : 1024} required disabled={busy || preview} onChange={(event) => setPassword(event.target.value)} />
              <button type="button" aria-label={t(showPassword ? "account.hidePassword" : "account.showPassword")} aria-pressed={showPassword} disabled={busy || preview} onClick={() => setShowPassword((shown) => !shown)}>{showPassword ? <EyeOff size={16} /> : <Eye size={16} />}</button>
            </span>
          </label>
          {registering && <label className="account-settings__label">{t("account.confirmPassword")}<input className="mem-input" name="confirmation" type="password" autoComplete="new-password" value={confirmation} maxLength={40} required disabled={busy} onChange={(event) => setConfirmation(event.target.value)} /></label>}
          {registering && options?.emailVerification && <>
            <label className="account-settings__label">{t("account.email")}<input className="mem-input" name="email" type="email" autoComplete="email" value={email} maxLength={254} required disabled={busy} onChange={(event) => setEmail(event.target.value)} /></label>
            <label className="account-settings__label">{t("account.emailCode")}<span className="account-settings__email-code"><input className="mem-input" name="code" autoComplete="one-time-code" value={code} maxLength={32} required disabled={busy} onChange={(event) => setCode(event.target.value)} /><button className="btn btn--small" type="button" disabled={busy || cooldown > 0 || !email.trim()} onClick={() => void perform(() => app.AccountSendRegistrationCode(email.trim()), (result) => { if (result.success) { setCooldown(60); setNotice("account.codeSent"); } else setError(result.errorCode ?? "rejected"); })}>{cooldown > 0 ? `${cooldown}s` : t("account.sendCode")}</button></span></label>
          </>}
          {registering && (options?.userAgreement || options?.privacyPolicy) && <div className="account-settings__legal"><label><input name="agreed" type="checkbox" checked={agreed} required disabled={busy} onChange={(event) => setAgreed(event.target.checked)} />{t("account.agreementRequired")}</label><div>{options.userAgreement && <button type="button" onClick={() => openExternal("https://sem.ensureok.ai/user-agreement")}>{t("account.agreement")}</button>}{options.privacyPolicy && <button type="button" onClick={() => openExternal("https://sem.ensureok.ai/privacy-policy")}>{t("account.privacy")}</button>}</div></div>}
        </>}
        {(!registering || canRegister) && <button className="btn btn--primary account-settings__submit" type="submit" disabled={busy || preview}>{t(busy ? "account.loading" : verifying ? "account.verify" : registering ? "account.register" : "account.login")}</button>}
        {registering && options?.websiteRequired && options.enabled && <button className="btn btn--primary" type="button" onClick={() => openExternal("https://sem.ensureok.ai/register")}>{t("account.console")}<ArrowUpRight size={14} /></button>}
        {!verifying && <p className="account-settings__switch"><span>{t(registering ? "account.haveAccount" : "account.noAccount")}</span><button type="button" disabled={busy} onClick={switchMode}>{t(registering ? "account.login" : "account.register")}</button></p>}
      </form>
    </div> : null}
    <div className="settings-section__actions account-settings__actions">
      {!verifying && !registering && (!startup || error || view?.hasSession || !view) && <button type="button" className="btn btn--small" disabled={busy} onClick={() => void run(() => app.AccountStatus())}>{t("account.refresh")}</button>}
      {(view?.hasSession || verifying || view?.warningCodes.includes("storage_delete")) && <button type="button" className="btn btn--small" disabled={busy} onClick={() => { clearSecrets(); void run(() => app.AccountLogout()); }}>{t(verifying ? "common.cancel" : "account.logout")}</button>}
    </div>
  </section>;
}
