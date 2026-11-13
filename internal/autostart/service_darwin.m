//go:build darwin && !ios && cgo

#import <AppKit/AppKit.h>
#import <ServiceManagement/ServiceManagement.h>
#include <string.h>

static void loginItemMessage(char **message, NSString *text) {
    if (message) *message = strdup(text.UTF8String);
}

// A go-run or test executable must never register the terminal/test runner.
static BOOL loginItemAvailable(char **message) {
    if (@available(macOS 13.0, *)) {
        NSBundle *bundle = NSBundle.mainBundle;
        if ([bundle.bundlePath.pathExtension isEqualToString:@"app"] && bundle.bundleIdentifier.length > 0) return YES;
        loginItemMessage(message, @"请使用打包后的 MiniProxy.app 设置登录自启");
    } else {
        loginItemMessage(message, @"登录自启需要 macOS 13 或更高版本");
    }
    return NO;
}

int miniproxyLoginItemStatus(char **message) {
    @autoreleasepool {
        if (!loginItemAvailable(message)) return -1;
        if (@available(macOS 13.0, *)) return (int)SMAppService.mainAppService.status;
        return -1;
    }
}

int miniproxyLoginItemSetEnabled(int enabled, char **message) {
    @autoreleasepool {
        if (!loginItemAvailable(message)) return -1;
        if (@available(macOS 13.0, *)) {
            SMAppService *service = SMAppService.mainAppService;
            SMAppServiceStatus before = service.status;
            if (enabled && (before == SMAppServiceStatusEnabled || before == SMAppServiceStatusRequiresApproval)) return (int)before;
            if (!enabled && (before == SMAppServiceStatusNotRegistered || before == SMAppServiceStatusNotFound)) return (int)SMAppServiceStatusNotRegistered;

            NSError *error = nil;
            BOOL success = enabled ? [service registerAndReturnError:&error] : [service unregisterAndReturnError:&error];
            SMAppServiceStatus after = service.status;
            // Registration may require consent, or another caller may have
            // already applied the requested state. Report the actual state.
            BOOL applied = enabled ? (after == SMAppServiceStatusEnabled || after == SMAppServiceStatusRequiresApproval)
                                   : after == SMAppServiceStatusNotRegistered;
            if (!success && !applied) {
                loginItemMessage(message, error.localizedDescription ?: @"无法更新登录项，请稍后重试");
            } else if (success && !applied) {
                loginItemMessage(message, @"macOS 尚未应用登录项设置，请重新检查状态");
            }
            return (int)after;
        }
        return -1;
    }
}

void miniproxyLoginItemOpenSettings(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (@available(macOS 13.0, *)) [SMAppService openSystemSettingsLoginItems];
    });
}
