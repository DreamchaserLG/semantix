package main

import (
	"context"

	"semantix/desktop/account"
)

func (a *App) accountClient() *account.Client {
	a.accountOnce.Do(func() { a.account = account.New() })
	return a.account
}

func (a *App) accountContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) AccountStatus() account.View { return a.accountClient().Status(a.accountContext()) }
func (a *App) AccountLogin(username, password string) account.View {
	return a.accountClient().Login(a.accountContext(), username, password)
}
func (a *App) AccountVerify(code string) account.View {
	return a.accountClient().Verify(a.accountContext(), code)
}
func (a *App) AccountLogout() account.View { return a.accountClient().Logout(a.accountContext()) }

func (a *App) AccountRegistrationOptions() account.RegistrationOptions {
	return a.accountClient().RegistrationOptions(a.accountContext())
}
func (a *App) AccountRegister(username, password, email, code string, agreed bool) account.RegistrationResult {
	return a.accountClient().Register(a.accountContext(), username, password, email, code, agreed)
}
func (a *App) AccountSendRegistrationCode(email string) account.RegistrationResult {
	return a.accountClient().SendRegistrationCode(a.accountContext(), email)
}
