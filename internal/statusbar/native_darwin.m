//go:build darwin && !ios && cgo

#import <Cocoa/Cocoa.h>

extern void loopStatusAction(char *data);
static NSStatusItem *proxyStatusItem;
static NSImage *proxyStatusIcon;
static NSMutableDictionary *proxyModeIcons;

void loopStatusAddIcon(void *png, int length) {
    @autoreleasepool {
        if (!proxyStatusIcon) {
            proxyStatusIcon = [[NSImage alloc] initWithSize:NSMakeSize(18,18)];
            proxyStatusIcon.template = YES;
        }
        NSData *data = [NSData dataWithBytes:png length:(NSUInteger)length];
        NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:data];
        if (rep) {
            rep.size = NSMakeSize(18,18);
            [proxyStatusIcon addRepresentation:rep];
        }
    }
}

// All three modes share the same silhouette and transparent lower-right
// cutout. Compose the glyph inside the 18-point image, at native pixel scales.
static NSImage *proxyIconForLetter(NSString *letter) {
    if (!letter) return proxyStatusIcon;
    if (!proxyModeIcons) proxyModeIcons = [[NSMutableDictionary alloc] init];
    NSImage *cached = proxyModeIcons[letter];
    if (cached) return cached;
    NSImage *image = [[[NSImage alloc] initWithSize:NSMakeSize(18,18)] autorelease];
    for (NSInteger scale = 1; scale <= 3; scale++) {
        NSBitmapImageRep *rep = [[[NSBitmapImageRep alloc]
            initWithBitmapDataPlanes:NULL pixelsWide:18*scale pixelsHigh:18*scale
            bitsPerSample:8 samplesPerPixel:4 hasAlpha:YES isPlanar:NO
            colorSpaceName:NSDeviceRGBColorSpace bytesPerRow:0 bitsPerPixel:0] autorelease];
        NSGraphicsContext *context = [NSGraphicsContext graphicsContextWithBitmapImageRep:rep];
        [NSGraphicsContext saveGraphicsState];
        [NSGraphicsContext setCurrentContext:context];
        CGContextScaleCTM(context.CGContext, scale, scale);
        CGContextTranslateCTM(context.CGContext, 0, -1);
        [proxyStatusIcon drawInRect:NSMakeRect(0,1,18,18) fromRect:NSZeroRect
                         operation:NSCompositingOperationSourceOver fraction:1];
        NSRectFillUsingOperation(NSMakeRect(10.5,0,7.5,10.5), NSCompositingOperationClear);
        [letter drawAtPoint:NSMakePoint(11,0) withAttributes:@{
            NSFontAttributeName:[NSFont monospacedSystemFontOfSize:9.5 weight:NSFontWeightSemibold],
            NSForegroundColorAttributeName:NSColor.blackColor
        }];
        [NSGraphicsContext restoreGraphicsState];
        rep.size = NSMakeSize(18,18);
        [image addRepresentation:rep];
    }
    image.template = YES;
    proxyModeIcons[letter] = image;
    return image;
}

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
    NSStatusBarButton *button = proxyStatusItem.button;
    NSString *letter = nil;
    if (letters) {
        NSString *mode = state[@"mode"];
        letter = [mode isEqualToString:@"global"] ? @"G" : ([mode isEqualToString:@"direct"] ? @"D" : @"R");
    }
    button.image = proxyIconForLetter(letter);
    button.imagePosition = NSImageOnly;
    button.title = @"";
    button.alphaValue = opacity;
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
        [proxyStatusIcon release]; proxyStatusIcon = nil;
        [proxyModeIcons release]; proxyModeIcons = nil;
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
