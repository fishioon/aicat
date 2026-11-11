//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <libproc.h>
#include <string.h>

char *aicat_client_app(int pid, char **bundleID) {
    @autoreleasepool {
        char buffer[PROC_PIDPATHINFO_MAXSIZE];
        if (proc_pidpath(pid, buffer, sizeof(buffer)) <= 0) {
            return NULL;
        }
        NSString *path = [NSString stringWithUTF8String:buffer];
        if (!path) {
            return NULL;
        }
        NSRange app = [path rangeOfString:@".app/" options:NSCaseInsensitiveSearch];
        if (app.location == NSNotFound) {
            return strdup(path.lastPathComponent.UTF8String);
        }
        // A bundled network helper belongs with its enclosing application.
        NSString *appPath = [path substringToIndex:NSMaxRange(app) - 1];
        NSBundle *bundle = [NSBundle bundleWithPath:appPath];
        NSString *identifier = bundle.bundleIdentifier;
        if (!identifier.length) {
            return strdup(path.lastPathComponent.UTF8String);
        }
        NSString *name = [bundle objectForInfoDictionaryKey:@"CFBundleDisplayName"];
        if (![name isKindOfClass:NSString.class] || !name.length) {
            name = appPath.lastPathComponent.stringByDeletingPathExtension;
        }
        *bundleID = strdup(identifier.UTF8String);
        return strdup(name.UTF8String);
    }
}
