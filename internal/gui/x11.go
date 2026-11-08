package gui

/*
#cgo pkg-config: gtk4 x11 webkitgtk-6.0
#include <gtk/gtk.h>
#include <gdk/x11/gdkx.h>
#include <X11/Xlib.h>
#include <webkit/webkit.h>

static GtkWidget *find_webview(GtkWidget *w) {
	if (WEBKIT_IS_WEB_VIEW(w)) return w;
	for (GtkWidget *c = gtk_widget_get_first_child(w); c != NULL; c = gtk_widget_get_next_sibling(c)) {
		GtkWidget *f = find_webview(c);
		if (f != NULL) return f;
	}
	return NULL;
}

// webview_origin is where the page's (0,0) is inside the window's X11 surface (CSD shadow and
// any widget above the web view included), in surface pixels before scaling, and the scale.
static void webview_origin(void *w, double *x, double *y, int *scale) {
	GtkWidget *win = GTK_WIDGET(w);
	GtkWidget *wv = find_webview(win);
	graphene_point_t p = GRAPHENE_POINT_INIT(0, 0), out = GRAPHENE_POINT_INIT(0, 0);
	if (wv != NULL && !gtk_widget_compute_point(wv, win, &p, &out)) out = p;
	double sx = 0, sy = 0;
	gtk_native_get_surface_transform(GTK_NATIVE(win), &sx, &sy);
	*x = out.x + sx;
	*y = out.y + sy;
	GdkSurface *s = gtk_native_get_surface(GTK_NATIVE(win));
	*scale = s != NULL ? gdk_surface_get_scale_factor(s) : 1;
}

// window_xid is the X11 window of a GtkWindow, 0 when it is not on X11 (Wayland) or not realized.
static unsigned long window_xid(void *w) {
	GdkSurface *s = gtk_native_get_surface(GTK_NATIVE(w));
	if (s == NULL || !GDK_IS_X11_SURFACE(s)) return 0;
	return gdk_x11_surface_get_xid(s);
}

// add_save_set puts xid in the save-set of GTK's own X connection, the one that created the app's
// windows: if the app dies, the X server gives the embedded window back to the desktop.
static int add_save_set(unsigned long xid) {
	GdkDisplay *d = gdk_display_get_default();
	if (d == NULL || !GDK_IS_X11_DISPLAY(d)) return -1;
	Display *x = gdk_x11_display_get_xdisplay(d);
	XAddToSaveSet(x, (Window)xid);
	XFlush(x);
	return 0;
}
*/
import "C"

import (
	"errors"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// windowXID is the X11 window of an app window; it runs on the UI thread. 0 on Wayland.
func windowXID(w *application.WebviewWindow) uint32 {
	var xid C.ulong
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			xid = C.window_xid(unsafe.Pointer(p))
		}
	})
	return uint32(xid)
}

// gtkSaveSet adds xid to the save-set of GTK's X connection, on the UI thread (design D6: the
// spike showed only that connection's save-set keeps the VM's window when the app is killed).
func gtkSaveSet(xid uint32) error {
	var rc C.int
	application.InvokeSync(func() { rc = C.add_save_set(C.ulong(xid)) })
	if rc != 0 {
		return errors.New("the app is not on X11")
	}
	return nil
}

// webviewOrigin is the page's origin in the window's X11 surface and the surface scale (UI thread).
func webviewOrigin(w *application.WebviewWindow) (x, y float64, scale int) {
	var cx, cy C.double
	var cs C.int = 1
	application.InvokeSync(func() {
		if p := w.NativeWindow(); p != nil {
			C.webview_origin(unsafe.Pointer(p), &cx, &cy, &cs)
		}
	})
	return float64(cx), float64(cy), int(cs)
}
