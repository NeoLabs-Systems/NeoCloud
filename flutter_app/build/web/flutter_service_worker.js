'use strict';
const MANIFEST = 'flutter-app-manifest';
const TEMP = 'flutter-temp-cache';
const CACHE_NAME = 'flutter-app-cache';

const RESOURCES = {"version.json": "274625624466aab09bab70a4121fe810",
"assets/shaders/ink_sparkle.frag": "ecc85a2e95f5e9f53123dcaf8cb9b6ce",
"assets/assets/branding/logo.svg": "07589ef5cde829593fc42c5ef3a3ab89",
"assets/assets/fonts/geist/Geist-SemiBold.ttf": "d6e2372e443f397b5cecf020786e70ce",
"assets/assets/fonts/geist/Geist-Medium.ttf": "42a119d5819da66b2d50fc5a9a60cd8c",
"assets/assets/fonts/geist/GeistMono-Medium.ttf": "95b51d96e6411fc484413dae2100387c",
"assets/assets/fonts/geist/GeistMono-SemiBold.ttf": "cedd25ea50a0028f90dc589fd7d53e6a",
"assets/assets/fonts/geist/Geist-ExtraBold.ttf": "89c90a781789a06ef1f8ccfed520b235",
"assets/assets/fonts/geist/GeistMono-Regular.ttf": "2e1101b6e017f1a7f1e78471a69885f0",
"assets/assets/fonts/geist/OFL-Geist.txt": "fe4cc681382eff30e91c042d87f245aa",
"assets/assets/fonts/geist/Geist-Bold.ttf": "7f2d53bc7c92339909ae26338926ac15",
"assets/assets/fonts/geist/OFL-GeistMono.txt": "fe4cc681382eff30e91c042d87f245aa",
"assets/assets/fonts/geist/GeistMono-Bold.ttf": "adb8793d87ca82d8488ef2aa49f2c9da",
"assets/assets/fonts/geist/Geist-Light.ttf": "0d9d99b9719e7a720cf5692692fc19af",
"assets/assets/fonts/geist/Geist-Regular.ttf": "91df64f7f79f4540ee1bbd6035919237",
"assets/assets/fonts/signing/OFL-sacramento.txt": "da638a63ec18b23dd769e1b50cd3437b",
"assets/assets/fonts/signing/DancingScript.ttf": "02ed13c946ba0e7c532000884562542d",
"assets/assets/fonts/signing/Sacramento-Regular.ttf": "c3264a2601f855aef183e1892c5504db",
"assets/assets/fonts/signing/OFL-greatvibes.txt": "9bf5586a982508628b07d3e43eb3eae2",
"assets/assets/fonts/signing/GreatVibes-Regular.ttf": "d1358c495233289490f6d9f1d41a3cc8",
"assets/assets/fonts/signing/OFL-dancingscript.txt": "311b662ed9014157c506041e46f68865",
"assets/assets/fonts/signing/Caveat.ttf": "ccf2f844ded8bfd50efbed532a60d068",
"assets/assets/fonts/signing/OFL-caveat.txt": "f041ff6e4f093a88573d3306e41037af",
"assets/fonts/MaterialIcons-Regular.otf": "977e52be44b6efc6fd6b39244b751597",
"assets/AssetManifest.bin.json": "cf27d33ddb08ba84a188bfa53b86e93b",
"assets/packages/pdfrx/assets/pdfium_worker.js": "5343e0f742afb6d7b63a9e3d178bc729",
"assets/packages/pdfrx/assets/pdfium.wasm": "70a0620e6434254fcb5b1f770db50bc6",
"assets/packages/pdfrx/assets/pdfium_client.js": "9ef7c314155dc36c966ef89df070dfd2",
"assets/FontManifest.json": "a8ed195afeedb6e60fa23243d90cdd3d",
"assets/AssetManifest.bin": "55d89cade1701c447a7b690650d4af18",
"assets/NOTICES": "e5afc001ea8e14a7d8413246ba9a2e76",
"assets/AssetManifest.json": "4ab8409df132f941570f1b46e7b7a8b9",
"flutter.js": "888483df48293866f9f41d3d9274a779",
"main.dart.js": "4d947905a14edbede5ccf8fec9738d8c",
"index.html": "326e569a9fbc70c1326b00f7b28bf340",
"/": "326e569a9fbc70c1326b00f7b28bf340",
"icons/Icon-maskable-192.png": "a1c9c782d47f8be75402914c492792d9",
"icons/Icon-192.png": "0de8705590faa7167d55d8c09ba625ce",
"icons/Icon-maskable-512.png": "9382e904095dddc4ede4ebdd9cf3f4dd",
"icons/Icon-512.png": "e35db7b717ded7339bf060da4f08d57d",
"favicon.png": "400010858d16b05ebb968a222f13a876",
"favicon.svg": "07589ef5cde829593fc42c5ef3a3ab89",
"flutter_bootstrap.js": "0ab43d8948ee7f6fbce13fd187e69469",
"canvaskit/skwasm_heavy.js": "413f5b2b2d9345f37de148e2544f584f",
"canvaskit/canvaskit.js.symbols": "58832fbed59e00d2190aa295c4d70360",
"canvaskit/canvaskit.wasm": "07b9f5853202304d3b0749d9306573cc",
"canvaskit/skwasm.js.symbols": "0088242d10d7e7d6d2649d1fe1bda7c1",
"canvaskit/skwasm.wasm": "264db41426307cfc7fa44b95a7772109",
"canvaskit/skwasm.js": "1ef3ea3a0fec4569e5d531da25f34095",
"canvaskit/skwasm_heavy.wasm": "8034ad26ba2485dab2fd49bdd786837b",
"canvaskit/skwasm_heavy.js.symbols": "3c01ec03b5de6d62c34e17014d1decd3",
"canvaskit/chromium/canvaskit.js.symbols": "193deaca1a1424049326d4a91ad1d88d",
"canvaskit/chromium/canvaskit.wasm": "24c77e750a7fa6d474198905249ff506",
"canvaskit/chromium/canvaskit.js": "5e27aae346eee469027c80af0751d53d",
"canvaskit/canvaskit.js": "140ccb7d34d0a55065fbd422b843add6",
"manifest.json": "b67c7a2f75bc71a809d3918bdb308145"};
// The application shell files that are downloaded before a service worker can
// start.
const CORE = ["main.dart.js",
"index.html",
"flutter_bootstrap.js",
"assets/AssetManifest.bin.json",
"assets/FontManifest.json"];

// During install, the TEMP cache is populated with the application shell files.
self.addEventListener("install", (event) => {
  self.skipWaiting();
  return event.waitUntil(
    caches.open(TEMP).then((cache) => {
      return cache.addAll(
        CORE.map((value) => new Request(value, {'cache': 'reload'})));
    })
  );
});
// During activate, the cache is populated with the temp files downloaded in
// install. If this service worker is upgrading from one with a saved
// MANIFEST, then use this to retain unchanged resource files.
self.addEventListener("activate", function(event) {
  return event.waitUntil(async function() {
    try {
      var contentCache = await caches.open(CACHE_NAME);
      var tempCache = await caches.open(TEMP);
      var manifestCache = await caches.open(MANIFEST);
      var manifest = await manifestCache.match('manifest');
      // When there is no prior manifest, clear the entire cache.
      if (!manifest) {
        await caches.delete(CACHE_NAME);
        contentCache = await caches.open(CACHE_NAME);
        for (var request of await tempCache.keys()) {
          var response = await tempCache.match(request);
          await contentCache.put(request, response);
        }
        await caches.delete(TEMP);
        // Save the manifest to make future upgrades efficient.
        await manifestCache.put('manifest', new Response(JSON.stringify(RESOURCES)));
        // Claim client to enable caching on first launch
        self.clients.claim();
        return;
      }
      var oldManifest = await manifest.json();
      var origin = self.location.origin;
      for (var request of await contentCache.keys()) {
        var key = request.url.substring(origin.length + 1);
        if (key == "") {
          key = "/";
        }
        // If a resource from the old manifest is not in the new cache, or if
        // the MD5 sum has changed, delete it. Otherwise the resource is left
        // in the cache and can be reused by the new service worker.
        if (!RESOURCES[key] || RESOURCES[key] != oldManifest[key]) {
          await contentCache.delete(request);
        }
      }
      // Populate the cache with the app shell TEMP files, potentially overwriting
      // cache files preserved above.
      for (var request of await tempCache.keys()) {
        var response = await tempCache.match(request);
        await contentCache.put(request, response);
      }
      await caches.delete(TEMP);
      // Save the manifest to make future upgrades efficient.
      await manifestCache.put('manifest', new Response(JSON.stringify(RESOURCES)));
      // Claim client to enable caching on first launch
      self.clients.claim();
      return;
    } catch (err) {
      // On an unhandled exception the state of the cache cannot be guaranteed.
      console.error('Failed to upgrade service worker: ' + err);
      await caches.delete(CACHE_NAME);
      await caches.delete(TEMP);
      await caches.delete(MANIFEST);
    }
  }());
});
// The fetch handler redirects requests for RESOURCE files to the service
// worker cache.
self.addEventListener("fetch", (event) => {
  if (event.request.method !== 'GET') {
    return;
  }
  var origin = self.location.origin;
  var key = event.request.url.substring(origin.length + 1);
  // Redirect URLs to the index.html
  if (key.indexOf('?v=') != -1) {
    key = key.split('?v=')[0];
  }
  if (event.request.url == origin || event.request.url.startsWith(origin + '/#') || key == '') {
    key = '/';
  }
  // If the URL is not the RESOURCE list then return to signal that the
  // browser should take over.
  if (!RESOURCES[key]) {
    return;
  }
  // If the URL is the index.html, perform an online-first request.
  if (key == '/') {
    return onlineFirst(event);
  }
  event.respondWith(caches.open(CACHE_NAME)
    .then((cache) =>  {
      return cache.match(event.request).then((response) => {
        // Either respond with the cached resource, or perform a fetch and
        // lazily populate the cache only if the resource was successfully fetched.
        return response || fetch(event.request).then((response) => {
          if (response && Boolean(response.ok)) {
            cache.put(event.request, response.clone());
          }
          return response;
        });
      })
    })
  );
});
self.addEventListener('message', (event) => {
  // SkipWaiting can be used to immediately activate a waiting service worker.
  // This will also require a page refresh triggered by the main worker.
  if (event.data === 'skipWaiting') {
    self.skipWaiting();
    return;
  }
  if (event.data === 'downloadOffline') {
    downloadOffline();
    return;
  }
});
// Download offline will check the RESOURCES for all files not in the cache
// and populate them.
async function downloadOffline() {
  var resources = [];
  var contentCache = await caches.open(CACHE_NAME);
  var currentContent = {};
  for (var request of await contentCache.keys()) {
    var key = request.url.substring(origin.length + 1);
    if (key == "") {
      key = "/";
    }
    currentContent[key] = true;
  }
  for (var resourceKey of Object.keys(RESOURCES)) {
    if (!currentContent[resourceKey]) {
      resources.push(resourceKey);
    }
  }
  return contentCache.addAll(resources);
}
// Attempt to download the resource online before falling back to
// the offline cache.
function onlineFirst(event) {
  return event.respondWith(
    fetch(event.request).then((response) => {
      return caches.open(CACHE_NAME).then((cache) => {
        cache.put(event.request, response.clone());
        return response;
      });
    }).catch((error) => {
      return caches.open(CACHE_NAME).then((cache) => {
        return cache.match(event.request).then((response) => {
          if (response != null) {
            return response;
          }
          throw error;
        });
      });
    })
  );
}
