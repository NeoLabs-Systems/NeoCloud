// Finder icon and sidebar calls for shellfolder_darwin.go.
#import <Cocoa/Cocoa.h>

// LSSharedFileList is deprecated but still the only API for the sidebar
// Favorites, and still works (checked on macOS 26).
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

int ncSetFolderIcon(const char *path, const void *png, int length) {
	@autoreleasepool {
		NSImage *image = [[NSImage alloc] initWithData:[NSData dataWithBytes:png length:length]];
		if (image == nil) {
			return 0;
		}
		return [[NSWorkspace sharedWorkspace] setIcon:image forFile:[NSString stringWithUTF8String:path] options:0];
	}
}

// ncSidebarEach calls found (if any) for every Favorites item that resolves to path
// and answers how many there were, or -1 without the list.
static int ncSidebarEach(const char *path, void (^found)(LSSharedFileListRef, LSSharedFileListItemRef)) {
	LSSharedFileListRef list = LSSharedFileListCreate(NULL, kLSSharedFileListFavoriteItems, NULL);
	if (list == NULL) {
		return -1;
	}
	NSString *want = [[NSURL fileURLWithPath:[NSString stringWithUTF8String:path]] URLByStandardizingPath].path;
	UInt32 seed;
	CFArrayRef items = LSSharedFileListCopySnapshot(list, &seed);
	int count = 0;
	for (CFIndex i = 0; items != NULL && i < CFArrayGetCount(items); i++) {
		LSSharedFileListItemRef item = (LSSharedFileListItemRef)CFArrayGetValueAtIndex(items, i);
		CFURLRef url = LSSharedFileListItemCopyResolvedURL(item, kLSSharedFileListNoUserInteraction | kLSSharedFileListDoNotMountVolumes, NULL);
		if (url == NULL) {
			continue;
		}
		if ([[(__bridge NSURL *)url URLByStandardizingPath].path isEqualToString:want]) {
			count++;
			if (found != nil) {
				found(list, item);
			}
		}
		CFRelease(url);
	}
	if (items != NULL) {
		CFRelease(items);
	}
	CFRelease(list);
	return count;
}

int ncSidebarAdd(const char *path) {
	@autoreleasepool {
		int existing = ncSidebarEach(path, nil);
		if (existing < 0) {
			return 0;
		}
		if (existing > 0) {
			return 1;
		}
		LSSharedFileListRef list = LSSharedFileListCreate(NULL, kLSSharedFileListFavoriteItems, NULL);
		if (list == NULL) {
			return 0;
		}
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path] isDirectory:YES];
		LSSharedFileListItemRef item = LSSharedFileListInsertItemURL(list, kLSSharedFileListItemLast, NULL, NULL, (__bridge CFURLRef)url, NULL, NULL);
		CFRelease(list);
		if (item == NULL) {
			return 0;
		}
		CFRelease(item);
		return 1;
	}
}

int ncSidebarRemove(const char *path) {
	@autoreleasepool {
		return ncSidebarEach(path, ^(LSSharedFileListRef list, LSSharedFileListItemRef item) {
			LSSharedFileListItemRemove(list, item);
		}) >= 0;
	}
}
