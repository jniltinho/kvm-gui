// Package gui is kvm-gui's desktop window on Wails v3: the embedded Vue / Vite Plus / Tailwind SPA
// (frontend/, built into dist/) in the system webview, kept on the app by the navigation guard,
// with App as the service the page calls. It is the only package that imports Wails.
package gui

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// dist is the built SPA, written by `make frontend` (frontend/ → internal/gui/dist). Kept out of git.
//
//go:embed all:dist
var assets embed.FS

// Options are the gui command's flags.
type Options struct {
	Version string
	// SafeGPU starts the webview with GPU acceleration off (--safe-gpu, KVM_GUI_SAFE_GPU=1).
	SafeGPU bool
	// SelfTest opens the Manager, waits until the page has loaded the VM list, and exits 0.
	SelfTest bool
}

// selfTestTimeout bounds --self-test: the page must report ready by then.
const selfTestTimeout = 60 * time.Second

// Run opens the Manager window and returns when the app quits.
func Run(o Options) error {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	svc := &App{version: o.Version}
	opts := application.Options{
		Name:        "kvm-gui",
		Description: "Create and run macOS and other VMs on Linux KVM",
		LogLevel:    slog.LevelInfo, // never Debug: Wails logs binding arguments there
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist), DisableLogging: true},
		Linux:       application.LinuxOptions{ProgramName: "kvm-gui"},
	}
	winOpts := application.WebviewWindowOptions{
		Title: "kvm-gui", Width: 1280, Height: 800, MinWidth: 800, MinHeight: 560,
		// The page needs none of these (the web trust boundary): deny them all.
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone:    application.PermissionDeny,
			application.PermissionCamera:        application.PermissionDeny,
			application.PermissionGeolocation:   application.PermissionDeny,
			application.PermissionNotifications: application.PermissionDeny,
			application.PermissionClipboardRead: application.PermissionDeny,
		},
	}
	if o.SafeGPU || os.Getenv("KVM_GUI_SAFE_GPU") == "1" {
		safeGraphics(&opts, &winOpts)
	}
	var win *application.WebviewWindow
	if !o.SelfTest {
		opts.SingleInstance = &application.SingleInstanceOptions{
			UniqueID: "io.github.jniltinho.kvm-gui",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if win != nil {
					win.Restore()
					win.Focus()
				}
			},
		}
	}
	app := application.New(opts)
	// --self-test: readiness and the timeout race for one result; the first one wins and quits.
	result := make(chan error, 1)
	if o.SelfTest {
		svc.ready = func() {
			select {
			case result <- nil:
				app.Quit()
			default:
			}
		}
		timer := time.AfterFunc(selfTestTimeout, func() {
			select {
			case result <- errors.New("self-test: the page did not load the VM list in time"):
				app.Quit()
			default:
			}
		})
		defer timer.Stop()
	}
	// The app's page loads only once the navigation guard is on.
	winOpts.URL, winOpts.HTML = guardedStart()
	win = app.Window.NewWithOptions(winOpts)
	var once sync.Once
	win.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
		once.Do(func() {
			if err := guardNavigation(win); err != nil {
				app.Logger.Error(err.Error())
				app.Dialog.Error().SetTitle("kvm-gui").SetMessage("The app cannot start safely: " + err.Error()).Show()
				app.Quit()
				return
			}
			if winOpts.URL == "" {
				win.SetURL("/")
			}
		})
	})
	if err := app.Run(); err != nil {
		return err
	}
	if o.SelfTest {
		select {
		case err := <-result:
			return err
		default:
			return errors.New("self-test: the window closed before the page was ready")
		}
	}
	return nil
}
