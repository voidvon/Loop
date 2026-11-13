//go:build darwin && !ios && cgo

#import <Cocoa/Cocoa.h>

extern void loopStatusAction(char *data);
static NSStatusItem *proxyStatusItem;

@interface LoopStatusDelegate : NSObject <NSApplicationDelegate>
- (void)invoke:(NSMenuItem *)sender;
@end

@implementation LoopStatusDelegate
- (void)invoke:(NSMenuItem *)sender {
    NSString *value = sender.representedObject;
    if (value) loopStatusAction((char *)value.UTF8String);
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    loopStatusAction("{\"action\":\"shutdown\"}");
    return NSTerminateCancel;
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)flag {
    loopStatusAction("{\"action\":\"show_ui\"}");
    return NO;
}
@end

static LoopStatusDelegate *proxyDelegate;

static NSMenu *proxyMenu(NSArray *items) {
    NSMenu *menu = [[[NSMenu alloc] initWithTitle:@"Loop"] autorelease];
    menu.autoenablesItems = NO;
    for (NSDictionary *value in items) {
        if ([value[@"separator"] boolValue]) { [menu addItem:NSMenuItem.separatorItem]; continue; }
        NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:value[@"label"] ?: @"" action:@selector(invoke:) keyEquivalent:@""] autorelease];
        item.target = proxyDelegate;
        item.enabled = [value[@"enabled"] boolValue];
        item.state = [value[@"checked"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
        if (value[@"action"]) {
            NSData *data = [NSJSONSerialization dataWithJSONObject:value[@"action"] options:0 error:nil];
            item.representedObject = [[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease];
        }
        if ([value[@"children"] count] > 0) item.submenu = proxyMenu(value[@"children"]);
        [menu addItem:item];
    }
    return menu;
}

static void proxyUpdate(NSString *json) {
    if (!proxyStatusItem) return;
    NSDictionary *model = [NSJSONSerialization JSONObjectWithData:[json dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];
    if (!model) return;
    NSDictionary *state = model[@"state"];
    NSDictionary *prefs = state[@"preferences"];
    BOOL letters = [prefs[@"show_mode_letter"] boolValue];
    CGFloat opacity = (![state[@"running"] boolValue] && [prefs[@"dim_disconnected"] boolValue]) ? 0.5 : 1.0;
    NSImage *image = [[[NSImage alloc] initWithSize:NSMakeSize(18,18)] autorelease];
    [image lockFocus];
    [[NSColor colorWithWhite:0 alpha:opacity] set];
    NSBezierPath *screen = [NSBezierPath bezierPathWithRect:letters ? NSMakeRect(1.5,6,10.5,8) : NSMakeRect(2,5,14,10)];
    screen.lineWidth = 1.5; [screen stroke];
    NSBezierPath *base = [NSBezierPath bezierPath];
    [base moveToPoint:NSMakePoint(letters?0:1,3.5)]; [base lineToPoint:NSMakePoint(letters?12:17,3.5)];
    base.lineWidth = 1.5; [base stroke];
    if (letters) {
        NSString *mode = state[@"mode"];
        NSString *letter = [mode isEqualToString:@"global"] ? @"G" : ([mode isEqualToString:@"direct"] ? @"D" : @"R");
        [letter drawAtPoint:NSMakePoint(10,0) withAttributes:@{NSFontAttributeName:[NSFont boldSystemFontOfSize:9],NSForegroundColorAttributeName:[NSColor colorWithWhite:0 alpha:opacity]}];
    }
    [image unlockFocus];
    image.template = YES;
    proxyStatusItem.button.image = image;
    proxyStatusItem.button.toolTip = [state[@"running"] boolValue] ? @"Loop · 已连接" : @"Loop · 未连接";
    proxyStatusItem.menu = proxyMenu(model[@"items"]);
}

void loopStatusRun(char *model) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        proxyDelegate = [[LoopStatusDelegate alloc] init];
        NSApp.delegate = proxyDelegate;
        proxyStatusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength] retain];
        proxyUpdate([NSString stringWithUTF8String:model]);
        [NSApp run];
        [[NSStatusBar systemStatusBar] removeStatusItem:proxyStatusItem];
        [proxyStatusItem release]; proxyStatusItem = nil;
        NSApp.delegate = nil;
        [proxyDelegate release]; proxyDelegate = nil;
    }
}

void loopStatusUpdate(char *model) {
    NSString *copy = [[NSString alloc] initWithUTF8String:model];
    dispatch_async(dispatch_get_main_queue(), ^{ proxyUpdate(copy); [copy release]; });
}

void loopStatusStop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0 context:nil subtype:0 data1:0 data2:0] atStart:NO];
    });
}
