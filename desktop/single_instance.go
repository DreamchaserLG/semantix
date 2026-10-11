package main

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func singleInstanceLock(app *App) *options.SingleInstanceLock {
	// Allow contributors to run a dev build alongside the installed app.
	// Set SEMANTIX_DEV=1 to skip the single-instance lock.
	if os.Getenv("SEMANTIX_DEV") != "" {
		return nil
	}
	return &options.SingleInstanceLock{
		UniqueId: singleInstanceID(),
		OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			app.secondInstanceLaunch()
			if app.ctx != nil && parseDesktopLaunchArgs(data.Args).OpenAccount {
				runtime.EventsEmit(app.ctx, "app:open-settings", "account")
			}
		},
	}
}
