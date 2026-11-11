//go:build darwin && cgo && !e2e

#import <Foundation/Foundation.h>
#import <SystemConfiguration/SystemConfiguration.h>

static char *scFailure(NSString *operation) {
    return strdup([[NSString stringWithFormat:@"%@: %s", operation, SCErrorString(SCError())] UTF8String]);
}

char *aicat_proxy_read(char **failure) {
    @autoreleasepool {
        SCPreferencesRef prefs = SCPreferencesCreate(NULL, CFSTR("aicat"), NULL);
        if (!prefs) { *failure = scFailure(@"读取系统网络配置"); return NULL; }
        CFArrayRef services = SCNetworkServiceCopyAll(prefs);
        SCNetworkSetRef current = SCNetworkSetCopyCurrent(prefs);
        CFArrayRef listed = current ? SCNetworkSetCopyServices(current) : NULL;
        NSMutableSet *ids = [NSMutableSet set];
        for (id item in (__bridge NSArray *)listed) {
            [ids addObject:(__bridge NSString *)SCNetworkServiceGetServiceID((__bridge SCNetworkServiceRef)item)];
        }
        if (listed) CFRelease(listed);
        if (current) CFRelease(current);
        NSMutableArray *result = [NSMutableArray array];
        for (id item in (__bridge NSArray *)services) {
            SCNetworkServiceRef service = (__bridge SCNetworkServiceRef)item;
            SCNetworkProtocolRef protocol = SCNetworkServiceCopyProtocol(service, kSCNetworkProtocolTypeProxies);
            if (!protocol) continue;
            NSDictionary *proxies = (__bridge NSDictionary *)SCNetworkProtocolGetConfiguration(protocol);
            [result addObject:@{@"id": (__bridge NSString *)SCNetworkServiceGetServiceID(service),
                                @"name": (__bridge NSString *)SCNetworkServiceGetName(service) ?: @"Network",
                                @"proxies": proxies ?: @{},
                                @"disabled": SCNetworkServiceGetEnabled(service) ? @NO : @YES,
                                @"unlisted": [ids containsObject:(__bridge NSString *)SCNetworkServiceGetServiceID(service)] ? @NO : @YES}];
            CFRelease(protocol);
        }
        if (services) CFRelease(services);
        CFRelease(prefs);
        NSError *error = nil;
        NSData *json = [NSJSONSerialization dataWithJSONObject:result options:0 error:&error];
        if (!json) { *failure = strdup(error.localizedDescription.UTF8String); return NULL; }
        return strdup([[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding] autorelease].UTF8String);
    }
}
