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

// The GLFW rendering view remains the input target and OpenGL surface. Put
// native materials behind it, rather than adding them over the drawn widgets.
static void loopInstallBackdrop(NSWindow *window, double sidebarWidth) {
    NSView *renderer = [window.contentView retain];
    NSRect bounds = renderer.bounds;
    NSView *host = [[NSView alloc] initWithFrame:bounds];
    host.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    // Behind-window materials must not overlap: AppKit would otherwise use
    // the lower material for the entire overlap, hiding the sidebar material.
    NSVisualEffectView *backdrop = [[NSVisualEffectView alloc] initWithFrame:NSMakeRect(sidebarWidth, 0, MAX(0, bounds.size.width - sidebarWidth), bounds.size.height)];
    backdrop.wantsLayer = YES;
    backdrop.material = NSVisualEffectMaterialUnderWindowBackground;
    backdrop.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    backdrop.state = NSVisualEffectStateActive;
    backdrop.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [host addSubview:backdrop];
    [backdrop release];

    NSVisualEffectView *sidebar = [[NSVisualEffectView alloc] initWithFrame:NSMakeRect(0, 0, sidebarWidth, bounds.size.height)];
    sidebar.wantsLayer = YES;
    sidebar.material = NSVisualEffectMaterialSidebar;
    sidebar.blendingMode = NSVisualEffectBlendingModeBehindWindow;
    sidebar.state = NSVisualEffectStateActive;
    sidebar.autoresizingMask = NSViewHeightSizable;
    [host addSubview:sidebar];
    [sidebar release];

    window.opaque = NO;
    window.backgroundColor = NSColor.clearColor;
    window.contentView = host;
    renderer.frame = host.bounds;
    renderer.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    [host addSubview:renderer];
    [window makeFirstResponder:renderer];
    [renderer release];
    [host release];
}

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
        loopInstallBackdrop(window, sidebarWidth);

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

void loopWindowAppearance(uintptr_t handle, int mode) {
    NSWindow *window = (NSWindow *)handle;
    dispatch_async(dispatch_get_main_queue(), ^{
        // A nil appearance inherits live system light/dark changes.
        window.appearance = mode == 1 ? [NSAppearance appearanceNamed:NSAppearanceNameAqua] :
                            mode == 2 ? [NSAppearance appearanceNamed:NSAppearanceNameDarkAqua] : nil;
    });
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
