//go:build darwin && !ios && cgo

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include <stdint.h>

// Keep the monitor with its window and remove it before Fyne destroys the
// window. The block does not retain the window, avoiding an ownership cycle.
@interface LoopWindowChrome : NSObject
@property(nonatomic, retain) id monitor;
@end

@implementation LoopWindowChrome
- (void)dealloc {
    if (_monitor) {
        [NSEvent removeMonitor:_monitor];
        [_monitor release];
    }
    [super dealloc];
}
@end

static char chromeKey;

void loopWindowConfigure(uintptr_t handle, double headerHeight, double controlsWidth, double sidebarWidth) {
    NSWindow *window = (NSWindow *)handle;
    void (^configure)(void) = ^{
        if (objc_getAssociatedObject(window, &chromeKey)) return;
        // The first frame is borderless. Restore regular window capabilities
        // in one main-thread transaction, keeping the system titlebar hidden.
        NSRect frame = window.frame;
        window.titleVisibility = NSWindowTitleHidden;
        window.titlebarAppearsTransparent = YES;
        window.styleMask |= NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                            NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable |
                            NSWindowStyleMaskFullSizeContentView;
        NSButton *close = [window standardWindowButton:NSWindowCloseButton];
        close.superview.hidden = YES;
        for (NSNumber *kind in @[@(NSWindowCloseButton), @(NSWindowMiniaturizeButton), @(NSWindowZoomButton)]) {
            [window standardWindowButton:kind.unsignedIntegerValue].hidden = YES;
        }
        window.hasShadow = YES;
        window.movableByWindowBackground = NO;
        window.collectionBehavior |= NSWindowCollectionBehaviorFullScreenPrimary;
        [window setFrame:frame display:NO];

        LoopWindowChrome *chrome = [[LoopWindowChrome alloc] init];
        __unsafe_unretained NSWindow *target = window;
        chrome.monitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown handler:^NSEvent *(NSEvent *event) {
            if (event.window != target) return event;
            NSPoint point = event.locationInWindow;
            // Main-page toolbar buttons now share this vertical band. Drag
            // only the blank part of the sidebar, never intercept their taps.
            if (point.y < target.contentView.bounds.size.height - headerHeight || point.x < controlsWidth || point.x >= sidebarWidth) return event;
            if (event.clickCount == 2) [target zoom:nil];
            else [target performWindowDragWithEvent:event];
            return nil;
        }];
        objc_setAssociatedObject(window, &chromeKey, chrome, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        [chrome release];
    };
    if ([NSThread isMainThread]) configure();
    else dispatch_async(dispatch_get_main_queue(), configure);
}

void loopWindowMinimize(uintptr_t handle) {
    NSWindow *window = (NSWindow *)handle;
    dispatch_async(dispatch_get_main_queue(), ^{ [window miniaturize:nil]; });
}

void loopWindowZoom(uintptr_t handle) {
    NSWindow *window = (NSWindow *)handle;
    dispatch_async(dispatch_get_main_queue(), ^{ [window zoom:nil]; });
}

void loopWindowCleanup(uintptr_t handle) {
    NSWindow *window = (NSWindow *)handle;
    dispatch_async(dispatch_get_main_queue(), ^{ objc_setAssociatedObject(window, &chromeKey, nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC); });
}

// Preserve the toolkit delegate for every event except explicit application
// termination. Returning Cancel lets the Go UI confirm drafts asynchronously.
extern void loopAppRequestQuit(void);
@interface LoopQuitDelegate : NSObject <NSApplicationDelegate>
@property(nonatomic, retain) id previous;
@end
@implementation LoopQuitDelegate
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    loopAppRequestQuit();
    return NSTerminateCancel;
}
- (BOOL)respondsToSelector:(SEL)selector {
    return [super respondsToSelector:selector] || [_previous respondsToSelector:selector];
}
- (id)forwardingTargetForSelector:(SEL)selector {
    if ([_previous respondsToSelector:selector]) return _previous;
    return [super forwardingTargetForSelector:selector];
}
- (void)dealloc { [_previous release]; [super dealloc]; }
@end

static LoopQuitDelegate *quitDelegate;
void loopAppInstallQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (quitDelegate) return;
        quitDelegate = [[LoopQuitDelegate alloc] init];
        quitDelegate.previous = NSApp.delegate;
        NSApp.delegate = quitDelegate;
    });
}
void loopAppRestoreQuit(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!quitDelegate) return;
        if (NSApp.delegate == quitDelegate) NSApp.delegate = quitDelegate.previous;
        [quitDelegate release]; quitDelegate = nil;
    });
}
