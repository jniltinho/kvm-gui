// Package gui is kvm-gui's desktop window on Wails v3: the embedded Vue / Vite Plus / Tailwind SPA
// (frontend/, built into dist/) in the system webview, kept on the app by the navigation guard,
// with App as the service the page calls. It is the only package that imports Wails.
package gui

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jniltinho/kvm-gui/internal/core"
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
	// Show opens this VM's window (id or name) too; a second launch asks the running app (--show).
	Show string
}

// selfTestTimeout bounds --self-test: the page must report ready by then.
const selfTestTimeout = 60 * time.Second

// Run opens the Manager window and returns when the app quits.
func Run(o Options) error {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	if err := core.RecoverSnapshots(); err != nil {
		slog.Warn("interrupted snapshot operations", "error", err)
	}
	svc := &App{version: o.Version}
	if s, err := newScreenServer(); err != nil {
		slog.Warn("the VM screen server did not start", "error", err)
	} else {
		svc.screen = s
		defer s.Close()
	}
	opts := application.Options{
		Name:        "kvm-gui",
		Description: "Create and run macOS and other VMs on Linux KVM",
		LogLevel:    slog.LevelInfo, // never Debug: Wails logs binding arguments there
		Services:    []application.Service{application.NewService(svc)},
		OnShutdown:  embeds.releaseAll, // embedded VM windows go back to the desktop first
		// The handler below lets embedded VM windows go before quitting: one cleanup, not two.
		DisableDefaultSignalHandler: true,
		Assets:                      application.AssetOptions{Handler: application.AssetFileServerFS(dist), DisableLogging: true},
		Linux:                       application.LinuxOptions{ProgramName: "kvm-gui"},
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
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				if key := showArg(d.Args); key != "" {
					showVM(svc, key)
					return
				}
				if win != nil {
					win.Restore()
					win.Focus()
				}
			},
		}
	}
	app := application.New(opts)
	svc.send = func(name string, data any) { app.Event.Emit(name, data) }
	svc.pick = func(title string, folder bool, filters [][2]string) (string, error) {
		d := app.Dialog.OpenFile().SetTitle(title).CanChooseFiles(!folder).CanChooseDirectories(folder).CanCreateDirectories(folder)
		for _, f := range filters {
			d.AddFilter(f[0], f[1])
		}
		if home, err := os.UserHomeDir(); err == nil {
			d.SetDirectory(home)
		}
		return d.PromptForSingleSelection()
	}
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
	// Every window loads the app's page (at path) only once its navigation guard is on.
	open := func(o application.WebviewWindowOptions, path string) *application.WebviewWindow {
		o.URL, o.HTML = guardedStart()
		o.Zoom = zoomOf(core.Preference("ui_scale")) // File › Preferences › Display, from the first paint
		w := app.Window.NewWithOptions(o)
		var once sync.Once
		w.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
			once.Do(func() {
				setWindowIcon(w)
				if err := guardNavigation(w); err != nil {
					app.Logger.Error(err.Error())
					app.Dialog.Error().SetTitle("kvm-gui").SetMessage("The app cannot start safely: " + err.Error()).Show()
					app.Quit()
					return
				}
				if o.URL == "" {
					w.SetURL(path)
				}
			})
		})
		return w
	}
	win = open(winOpts, "/")
	svc.zoom = func(f float64) {
		for _, w := range app.Window.GetAll() {
			w.SetZoom(f)
		}
	}
	// A window per VM, named after it: Show and Normal start open it or bring it to the front.
	var showMu sync.Mutex
	svc.show = func(id, title string) {
		showMu.Lock()
		defer showMu.Unlock()
		if w, ok := app.Window.GetByName("vm:" + id); ok {
			w.Restore()
			w.Show()
			w.Focus()
			return
		}
		o := winOpts
		o.Name, o.Title, o.Width, o.Height, o.MinWidth, o.MinHeight = "vm:"+id, title, 1280, 860, 480, 320
		w := open(o, "/#/vm/"+url.PathEscape(id))
		// The embedded VM window goes back to the desktop before this window is destroyed.
		w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { embeds.dropHost(id, "vm") })
	}
	if o.Show != "" {
		var once sync.Once
		win.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) { once.Do(func() { showVM(svc, o.Show) }) })
	}
	// The Manager's Screen tab gives its VM windows back to the desktop before the Manager goes.
	win.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) { embeds.dropHost("", "manager") })
	svc.mainWindow = func() *application.WebviewWindow { return win }
	svc.vmWindow = func(id string) *application.WebviewWindow {
		if w, ok := app.Window.GetByName("vm:" + id); ok {
			if ww, ok := w.(*application.WebviewWindow); ok {
				return ww
			}
		}
		return nil
	}
	// Embedded VM windows go back to the desktop before the app exits, a signal included (the
	// save-set covers a SIGKILL).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sig)
	go func() {
		if _, ok := <-sig; ok {
			embeds.releaseAll()
			app.Quit()
		}
	}()
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

// showArg is --show's value in a second launch's arguments ("--show X" or "--show=X").
func showArg(args []string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--show="); ok {
			return v
		}
		if a == "--show" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// showVM opens the window of the VM key names (id or name).
func showVM(svc *App, key string) {
	vms, err := core.ListVMs()
	if err != nil {
		slog.Warn("--show", "error", err)
		return
	}
	e, ok := core.FindVM(vms, key)
	if !ok {
		slog.Warn("--show: no such VM", "vm", key)
		return
	}
	if err := svc.Show(e.ID); err != nil {
		slog.Warn("--show", "vm", key, "error", err)
	}
}
