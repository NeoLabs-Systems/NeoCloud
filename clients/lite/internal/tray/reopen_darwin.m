// The reopen event handler for reopen_darwin.go.
#import <Cocoa/Cocoa.h>

extern void goReopen(void);

@interface NCReopenHandler : NSObject
@end

@implementation NCReopenHandler
- (void)handleReopen:(NSAppleEventDescriptor *)event withReplyEvent:(NSAppleEventDescriptor *)reply {
	goReopen();
}
@end

static NCReopenHandler *reopenHandler;

// Replaces AppKit's own handler, so it runs on the main queue after the
// app finished launching.
void installReopenHandler(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		reopenHandler = [NCReopenHandler new];
		[[NSAppleEventManager sharedAppleEventManager] setEventHandler:reopenHandler
			andSelector:@selector(handleReopen:withReplyEvent:)
			forEventClass:kCoreEventClass
			andEventID:kAEReopenApplication];
	});
}
