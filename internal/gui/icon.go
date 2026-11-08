package gui

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>
#include <stdlib.h>

// set_icon adds dir to the icon theme's search path and makes "kvm-gui" the icon of window w:
// GTK 4 takes window icons only by name from the icon theme, and publishes them (_NET_WM_ICON on
// X11) itself, replacing any set from outside.
static void set_icon(void *w, const char *dir) {
	GdkDisplay *d = gdk_display_get_default();
	if (d != NULL && dir[0] != 0) gtk_icon_theme_add_search_path(gtk_icon_theme_get_for_display(d), dir);
	gtk_window_set_default_icon_name("kvm-gui");
	if (w != NULL) gtk_window_set_icon_name(GTK_WINDOW(w), "kvm-gui");
}
*/
import "C"

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"

	appassets "github.com/jniltinho/kvm-gui/internal/assets"
	"github.com/jniltinho/kvm-gui/internal/core"
)

var iconDir struct {
	once sync.Once
	dir  string
}

// appIconDir writes the embedded logo (SVG, and PNG for systems without an SVG loader) in the
// hicolor layout into the cache, once per launch: the binary and the AppImage run without the
// icon the packages install.
func appIconDir() string {
	iconDir.once.Do(func() {
		dir := filepath.Join(core.CacheDir(), "icons")
		for name, data := range map[string][]byte{"hicolor/scalable/apps/kvm-gui.svg": appassets.Logo, "hicolor/256x256/apps/kvm-gui.png": appassets.LogoPNG} {
			p := filepath.Join(dir, name)
			if cur, err := os.ReadFile(p); err != nil || !bytes.Equal(cur, data) {
				if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, data, 0o644) != nil {
					return
				}
			}
		}
		iconDir.dir = dir
	})
	return iconDir.dir
}

// setWindowIcon gives an app window the logo (title bar, task switcher), on the UI thread.
func setWindowIcon(w *application.WebviewWindow) {
	dir := C.CString(appIconDir())
	defer C.free(unsafe.Pointer(dir))
	application.InvokeSync(func() {
		var p unsafe.Pointer
		if n := w.NativeWindow(); n != nil {
			p = unsafe.Pointer(n)
		}
		C.set_icon(p, dir)
	})
}
