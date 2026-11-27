package backups

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type s3Fixture struct {
	mu           sync.Mutex
	objects      map[string][]byte
	storageClass map[string]string
	restoreState map[string]string
	restoreCalls []string
}

func newS3Fixture() *s3Fixture {
	return &s3Fixture{objects: map[string][]byte{}, storageClass: map[string]string{}, restoreState: map[string]string{}}
}

func (f *s3Fixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(path.Clean(r.URL.Path), "/archive")
	if key == "." {
		key = ""
	}
	key = strings.TrimPrefix(key, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodHead && key == "":
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodHead:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if class := f.storageClass[key]; class != "" {
			w.Header().Set("x-amz-storage-class", class)
		}
		if state := f.restoreState[key]; state == "ongoing" {
			w.Header().Set("x-amz-restore", `ongoing-request="true"`)
		} else if state == "available" {
			w.Header().Set("x-amz-restore", `ongoing-request="false", expiry-date="Fri, 21 Dec 2026 00:00:00 GMT"`)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		prefix := r.URL.Query().Get("prefix")
		objects := make([]struct {
			Key          string `xml:"Key"`
			LastModified string `xml:"LastModified"`
			ETag         string `xml:"ETag"`
			Size         int    `xml:"Size"`
			StorageClass string `xml:"StorageClass"`
		}, 0)
		for objectKey, data := range f.objects {
			if strings.HasPrefix(objectKey, prefix) {
				objects = append(objects, struct {
					Key          string `xml:"Key"`
					LastModified string `xml:"LastModified"`
					ETag         string `xml:"ETag"`
					Size         int    `xml:"Size"`
					StorageClass string `xml:"StorageClass"`
				}{objectKey, "2026-01-01T00:00:00Z", `"etag"`, len(data), f.storageClass[objectKey]})
			}
		}
		// Encode the object list directly to keep the fixture response compact.
		var body strings.Builder
		body.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>archive</Name><Prefix>`)
		_ = xml.EscapeText(&body, []byte(prefix))
		body.WriteString(`</Prefix><KeyCount>` + strconv.Itoa(len(objects)) + `</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated>`)
		for _, item := range objects {
			body.WriteString(`<Contents><Key>`)
			_ = xml.EscapeText(&body, []byte(item.Key))
			body.WriteString(`</Key><LastModified>` + item.LastModified + `</LastModified><ETag>` + item.ETag + `</ETag><Size>` + strconv.Itoa(item.Size) + `</Size><StorageClass>` + item.StorageClass + `</StorageClass></Contents>`)
		}
		body.WriteString(`</ListBucketResult>`)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, body.String())
	case r.Method == http.MethodPut:
		if source := r.Header.Get("X-Amz-Copy-Source"); source != "" {
			source, _ = url.PathUnescape(strings.TrimPrefix(source, "/"))
			source = strings.TrimPrefix(source, "archive/")
			data, ok := f.objects[source]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			f.objects[key] = append([]byte(nil), data...)
			f.storageClass[key] = r.Header.Get("X-Amz-Storage-Class")
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"etag"</ETag><LastModified>2026-01-01T00:00:00Z</LastModified></CopyObjectResult>`)
			return
		}
		data, _ := io.ReadAll(r.Body)
		f.objects[key] = data
		f.storageClass[key] = r.Header.Get("X-Amz-Storage-Class")
		w.Header().Set("ETag", `"etag"`)
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		var request struct {
			Objects []struct {
				Key string `xml:"Key"`
			} `xml:"Object"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&request)
		for _, object := range request.Objects {
			delete(f.objects, object.Key)
			delete(f.storageClass, object.Key)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`)
	case r.Method == http.MethodPost && r.URL.Query().Has("restore"):
		var request struct {
			Days    int `xml:"Days"`
			Glacier struct {
				Tier string `xml:"Tier"`
			} `xml:"GlacierJobParameters"`
		}
		_ = xml.NewDecoder(r.Body).Decode(&request)
		f.restoreCalls = append(f.restoreCalls, key+":"+request.Glacier.Tier+":"+strconv.Itoa(request.Days))
		f.restoreState[key] = "ongoing"
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		delete(f.storageClass, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected S3 fixture request", http.StatusNotImplemented)
	}
}

func TestS3TargetListsAndChangesOnlyOwnedObjectStorageClass(t *testing.T) {
	fixture := newS3Fixture()
	fixture.objects["tenant/backups/blobs/one.enc"] = []byte("blob")
	fixture.storageClass["tenant/backups/blobs/one.enc"] = "STANDARD"
	fixture.objects["tenant/backups/snapshots/backup.db"] = []byte("snapshot")
	fixture.storageClass["tenant/backups/snapshots/backup.db"] = "GLACIER"
	fixture.objects["tenant/backups/other/file"] = []byte("untouched")
	fixture.storageClass["tenant/backups/other/file"] = "STANDARD"
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewS3TargetWithHTTPClient(S3Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret",
		Prefix: "tenant/backups", ForcePathStyle: true, StorageClass: "STANDARD_IA",
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	objects, err := target.ListObjects(context.Background(), "blobs/")
	if err != nil || len(objects) != 1 || objects[0].Key != "blobs/one.enc" || objects[0].Size != 4 || objects[0].StorageClass != "STANDARD" {
		t.Fatalf("objects=%+v err=%v", objects, err)
	}
	if err = target.ChangeStorageClass(context.Background(), objects[0].Key, objects[0].Size); err != nil {
		t.Fatalf("change class: %v", err)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.storageClass["tenant/backups/blobs/one.enc"] != "STANDARD_IA" || string(fixture.objects["tenant/backups/blobs/one.enc"]) != "blob" {
		t.Fatalf("copied object class=%q bytes=%q", fixture.storageClass["tenant/backups/blobs/one.enc"], fixture.objects["tenant/backups/blobs/one.enc"])
	}
	if fixture.storageClass["tenant/backups/snapshots/backup.db"] != "GLACIER" || fixture.storageClass["tenant/backups/other/file"] != "STANDARD" {
		t.Fatal("storage-class copy modified an archived object or an unrelated target key")
	}
}

func TestS3TargetRequestsArchivedObjectRestoreAndReportsHydration(t *testing.T) {
	fixture := newS3Fixture()
	fixture.objects["tenant/glacier"] = []byte("a")
	fixture.storageClass["tenant/glacier"] = "GLACIER"
	fixture.objects["tenant/deep"] = []byte("b")
	fixture.storageClass["tenant/deep"] = "DEEP_ARCHIVE"
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewS3TargetWithHTTPClient(S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret", Prefix: "tenant", ForcePathStyle: true}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	ctx := context.Background()
	for _, item := range []struct{ key, tier string }{{"glacier", "Standard"}, {"deep", "Bulk"}} {
		stat, statErr := target.StatObject(ctx, item.key)
		if statErr != nil || !stat.Exists || stat.StorageClass == "" || stat.Restore != "" {
			t.Fatalf("initial stat %+v err=%v", stat, statErr)
		}
		if err = target.RequestObjectRestore(ctx, item.key, item.tier); err != nil {
			t.Fatal(err)
		}
		stat, err = target.StatObject(ctx, item.key)
		if err != nil || stat.Restore != "ongoing" {
			t.Fatalf("ongoing stat %+v err=%v", stat, err)
		}
		fixture.mu.Lock()
		fixture.restoreState["tenant/"+item.key] = "available"
		fixture.mu.Unlock()
		stat, err = target.StatObject(ctx, item.key)
		if err != nil || stat.Restore != "available" {
			t.Fatalf("available stat %+v err=%v", stat, err)
		}
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.restoreCalls) != 2 || fixture.restoreCalls[0] != "tenant/glacier:Standard:7" || fixture.restoreCalls[1] != "tenant/deep:Bulk:7" {
		t.Fatalf("restore calls %v", fixture.restoreCalls)
	}
}

func TestStorageClassMigratorPersistsProgressAndSkipsArchivedObjects(t *testing.T) {
	fixture := newS3Fixture()
	fixture.objects["tenant/backups/blobs/a"] = []byte("one")
	fixture.storageClass["tenant/backups/blobs/a"] = "STANDARD"
	fixture.objects["tenant/backups/snapshots/b"] = []byte("two")
	fixture.storageClass["tenant/backups/snapshots/b"] = "GLACIER"
	server := httptest.NewServer(fixture)
	defer server.Close()
	stateDir := t.TempDir()
	migrator, err := NewStorageClassMigrator(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	err = migrator.Start(7, S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret", Prefix: "tenant/backups", ForcePathStyle: true, StorageClass: "STANDARD_IA"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		value, statusErr := migrator.Status(7)
		if statusErr != nil {
			t.Fatal(statusErr)
		}
		state, ok := value.(StorageClassMigration)
		if ok && state.Status == "ok" {
			if state.ObjectsDone != 1 || state.ObjectsSkipped != 0 || state.ObjectsArchived != 1 || state.BytesDone != 3 || state.BytesTotal == nil || *state.BytesTotal != 3 {
				t.Fatalf("migration state=%+v", state)
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if fixture.storageClass["tenant/backups/blobs/a"] != "STANDARD_IA" || fixture.storageClass["tenant/backups/snapshots/b"] != "GLACIER" {
				t.Fatal("migration failed to move standard data or changed an archived object")
			}
			migrator.Close()
			reopened, reopenErr := NewStorageClassMigrator(stateDir)
			if reopenErr != nil {
				t.Fatal(reopenErr)
			}
			defer reopened.Close()
			persisted, persistErr := reopened.Status(7)
			if persistErr != nil || persisted == nil {
				t.Fatalf("progress missing after manager restart: value=%v err=%v", persisted, persistErr)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	state, _ := migrator.Status(7)
	t.Fatalf("migration did not finish: %#v", state)
}

func TestS3TargetValidatesConfiguration(t *testing.T) {
	client := &http.Client{}
	for _, config := range []S3Config{
		{},
		{Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret", StorageClass: "UNKNOWN"},
		{Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret", Endpoint: "file:///tmp"},
	} {
		if _, err := NewS3TargetWithHTTPClient(config, client); err == nil {
			t.Fatalf("invalid config accepted: %+v", config)
		}
	}
}

func TestS3TargetSnapshotObjectOperationsAndStorageClass(t *testing.T) {
	fixture := newS3Fixture()
	server := httptest.NewServer(fixture)
	defer server.Close()
	target, err := NewS3TargetWithHTTPClient(S3Config{
		Endpoint: server.URL, Region: "us-east-1", Bucket: "archive", AccessKeyID: "access", SecretAccessKey: "secret",
		Prefix: "/tenant/backups/", ForcePathStyle: true, StorageClass: "GLACIER",
	}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = target.Open(ctx); err != nil {
		t.Fatalf("open bucket: %v", err)
	}
	source := t.TempDir() + "/snapshot.enc"
	want := []byte("encrypted snapshot payload")
	if err = os.WriteFile(source, want, 0600); err != nil {
		t.Fatal(err)
	}
	key := "snapshots/2026-01-01T00-00-00.000Z/neocloud.db.enc"
	if err = target.Put(ctx, key, source); err != nil {
		t.Fatalf("put snapshot: %v", err)
	}
	fixture.mu.Lock()
	class := fixture.storageClass["tenant/backups/"+key]
	fixture.mu.Unlock()
	if class != "GLACIER" {
		t.Fatalf("uploaded storage class=%q", class)
	}
	if exists, existsErr := target.Exists(ctx, key); existsErr != nil || !exists {
		t.Fatalf("exists=%v err=%v", exists, existsErr)
	}
	listed, err := target.List(ctx, "snapshots")
	if err != nil || len(listed) != 1 || listed[0] != key {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	destination := t.TempDir() + "/download.enc"
	if err = target.Get(ctx, key, destination); err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != string(want) {
		t.Fatalf("download=%q err=%v", got, err)
	}
	if err = target.DeletePrefix(ctx, "snapshots"); err != nil {
		t.Fatalf("delete prefix: %v", err)
	}
	if exists, existsErr := target.Exists(ctx, key); existsErr != nil || exists {
		t.Fatalf("exists after prefix delete=%v err=%v", exists, existsErr)
	}
}

func TestS3TargetRejectsKeysOutsideConfiguredPrefix(t *testing.T) {
	target, err := NewS3TargetWithHTTPClient(S3Config{Bucket: "bucket", AccessKeyID: "key", SecretAccessKey: "secret", Prefix: "tenant"}, &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = target.key("../outside"); err == nil {
		t.Fatal("parent traversal accepted")
	}
}

func TestSchedulerRunsS3Target(t *testing.T) {
	db, keys, runner, _ := schedulerFixture(t)
	fixture := newS3Fixture()
	server := httptest.NewServer(fixture)
	defer server.Close()
	config, err := json.Marshal(map[string]any{
		"endpoint": server.URL, "region": "us-east-1", "bucket": "archive",
		"accessKeyId": "access", "secretAccessKey": "secret", "prefix": "scheduled",
		"forcePathStyle": true, "storageClass": "STANDARD_IA",
	})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := keys.EncryptValue(string(config))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO backup_targets(id,name,type,config_enc,schedule,hour,weekday,retention,enabled) VALUES(1,'s3','s3',?,'daily',?,0,2,1)`, sealed, (time.Now().Hour()+23)%24); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner.Start(ctx)
	defer runner.Close()
	runner.checkSchedules(ctx, time.Now())
	var runID int64
	if err = db.W.QueryRow(`SELECT id FROM backup_runs WHERE target_id=1 ORDER BY id DESC LIMIT 1`).Scan(&runID); err != nil {
		t.Fatalf("scheduled S3 run missing: %v", err)
	}
	waitRun(t, db.W, runID)
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if _, ok := fixture.objects["scheduled/snapshots"]; ok {
		t.Fatal("unexpected directory marker object")
	}
	if len(fixture.objects) != 2 {
		t.Fatalf("scheduled S3 objects=%v", fixture.objects)
	}
	for key := range fixture.objects {
		if !strings.HasPrefix(key, "scheduled/snapshots/") {
			t.Fatalf("scheduled S3 object escaped prefix: %s", key)
		}
	}
}
