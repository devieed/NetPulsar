//go:build darwin

package desktop

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Cocoa
#cgo darwin LDFLAGS: -framework UniformTypeIdentifiers
#import <Cocoa/Cocoa.h>
#include <string.h>

typedef struct {
	int x, y, w, h;
	int primary;
} NPMon;

typedef struct {
	int cx, cy;
	int down;
	int ok;
	int wx, wy, ww, wh;
	int hasWin;
	int count;
	int knowsHit;
	int onWidget;
	NPMon mons[8];
} NPInfo;

static int primaryHeight(void) {
	NSArray<NSScreen *> *screens = [NSScreen screens];
	if (screens.count == 0) {
		return 0;
	}
	return (int)NSHeight(screens[0].frame);
}

static void npQuery(NPInfo *info) {
	memset(info, 0, sizeof(*info));
	void (^block)(void) = ^{
		NSArray<NSScreen *> *screens = [NSScreen screens];
		if (screens.count == 0) {
			return;
		}
		int ph = primaryHeight();
		int n = (int)screens.count;
		if (n > 8) {
			n = 8;
		}
		info->count = n;
		for (int i = 0; i < n; i++) {
			NSRect f = [screens[i] visibleFrame];
			info->mons[i].x = (int)f.origin.x;
			info->mons[i].y = ph - (int)(f.origin.y + f.size.height);
			info->mons[i].w = (int)f.size.width;
			info->mons[i].h = (int)f.size.height;
			info->mons[i].primary = i == 0 ? 1 : 0;
		}
		NSPoint p = [NSEvent mouseLocation];
		info->cx = (int)p.x;
		info->cy = ph - (int)p.y;
		info->down = ([NSEvent pressedMouseButtons] & 1) ? 1 : 0;
		info->ok = 1;
		NSWindow *win = [NSApp mainWindow];
		if (win != nil) {
			NSInteger under = [NSWindow windowNumberAtPoint:p belowWindowWithWindowNumber:0];
			info->knowsHit = 1;
			info->onWidget = under == (NSInteger)win.windowNumber ? 1 : 0;
			NSRect f = [win frame];
			info->wx = (int)f.origin.x;
			info->wy = ph - (int)(f.origin.y + f.size.height);
			info->ww = (int)f.size.width;
			info->wh = (int)f.size.height;
			info->hasWin = 1;
		}
	};
	if ([NSThread isMainThread]) {
		block();
	} else {
		dispatch_sync(dispatch_get_main_queue(), block);
	}
}
*/
import "C"

// Raise is unused on macOS; the window manager owns stacking.
func Raise(title string, topmost bool) {
	_, _ = title, topmost
}

// HideFromTaskbar is unused on macOS.
func HideFromTaskbar(title string) { _ = title }

func Query(title string) Info {
	_ = title
	var q C.NPInfo
	C.npQuery(&q)
	info := Info{
		CursorX:        int(q.cx),
		CursorY:        int(q.cy),
		CursorOK:       q.ok == 1,
		MouseDown:      q.down == 1,
		KnowsHit:       q.knowsHit == 1,
		CursorOnWidget: q.onWidget == 1,
		Scale:          1,
	}
	for i := 0; i < int(q.count); i++ {
		m := q.mons[i]
		info.Monitors = append(info.Monitors, Monitor{
			Work:    Rect{int(m.x), int(m.y), int(m.w), int(m.h)},
			Scale:   1,
			Primary: m.primary == 1,
		})
	}
	if q.hasWin == 1 {
		info.HasWindow = true
		info.Window = Rect{int(q.wx), int(q.wy), int(q.ww), int(q.wh)}
	}
	return info
}
