import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { Minus, Square, X } from "lucide-react";
import { AccountSessionContext } from "../lib/accountSession";
import { app } from "../lib/bridge";
import { normalizeLangPref, useI18n } from "../lib/i18n";
import { normalizeThemePreference, normalizeThemeStyleForTheme } from "../lib/theme";
import { applyConfiguredBaseAppearance } from "../lib/themePack";
import type { AccountView } from "../lib/types";

const AccountPage = lazy(() => import("./AccountSettingsPage").then((module) => ({ default: module.AccountSettingsPage })));

export function AccountEntry({ children }: { children: ReactNode }) {
  const [view, setView] = useState<AccountView | null>(null);
  const { t, setPref } = useI18n();
  useEffect(() => {
    let cancelled = false;
    void app.DesktopStartupSettings().then((settings) => {
      if (cancelled) return;
      setPref(normalizeLangPref(settings.desktopLanguage));
      const theme = normalizeThemePreference(settings.desktopTheme);
      applyConfiguredBaseAppearance(theme, normalizeThemeStyleForTheme(settings.desktopThemeStyle, theme));
    }).catch(() => undefined);
    return () => { cancelled = true; };
  }, [setPref]);

  return <AccountSessionContext.Provider value={setView}>
    {view?.state === "signed_in" && view.user ? children : <main className="account-entry">
      <div className="account-entry__titlebar" onDoubleClick={() => void app.ToggleMaximiseMainWindow()} />
      {document.documentElement.dataset.platform === "windows" && <div className="windows-window-controls" aria-label="Window controls">
        <button className="windows-window-control" type="button" aria-label="Minimize window" onClick={() => void app.MinimiseMainWindow()}><Minus size={13} /></button>
        <button className="windows-window-control" type="button" aria-label="Maximize or restore window" onClick={() => void app.ToggleMaximiseMainWindow()}><Square size={11} /></button>
        <button className="windows-window-control windows-window-control--close" type="button" aria-label={t("common.close")} onClick={() => void app.CloseMainWindow()}><X size={13} /></button>
      </div>}
      <div className="account-entry__body">
        <div className="account-entry__brand"><img src={new URL("../assets/logo-mark.svg", import.meta.url).href} alt="" draggable={false} /><span>Build with Semantix</span></div>
        <Suspense fallback={<p role="status">{t("account.loading")}</p>}><AccountPage startup /></Suspense>
      </div>
      <footer className="account-entry__footer">Semantix</footer>
    </main>}
  </AccountSessionContext.Provider>;
}
