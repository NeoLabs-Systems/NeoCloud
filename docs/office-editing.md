# Editing office documents in the browser

NeoCloud can open Word, Excel and PowerPoint style files (`docx`, `xlsx`,
`pptx`, `odt`, `ods`, `odp`) in an editor in the browser, several people at
once. The editor is a separate server you run yourself:
[Collabora Online](https://www.collaboraonline.com/code/) (CODE). NeoCloud
speaks the WOPI protocol to it and stays the only place the files live:
they are stored encrypted as always, every save is a new version, and quota,
sharing and the activity journal work as for any upload.

Nothing changes for anyone until an admin turns it on. Plain text and CSV
files keep opening in the built-in text editor.

## 1. Run Collabora CODE

Two web addresses are involved, and both must be HTTPS when NeoCloud is:

- `https://cloud.example.com`: NeoCloud (as people reach it)
- `https://office.example.com`: the editor

```yaml
# compose.yaml (next to NeoCloud)
services:
  collabora:
    image: collabora/code
    restart: unless-stopped
    cap_add: [MKNOD]
    environment:
      # Which hosts may call back for files (a regex; dots escaped). The
      # address the editor uses to reach NeoCloud must match.
      aliasgroup1: https://cloud.example.com:443
      # Who may embed the editor in a page: NeoCloud.
      extra_params: >-
        --o:ssl.enable=false --o:ssl.termination=true
        --o:net.frame_ancestors=https://cloud.example.com
      # Optional: an admin login for Collabora's own admin console.
      # username: admin
      # password: change-me
    ports:
      - "127.0.0.1:9980:9980"
```

`ssl.enable=false` with `ssl.termination=true` is for a reverse proxy that
speaks HTTPS to browsers; without a proxy, give Collabora its own
certificate instead. If NeoCloud runs on the same Docker network, the editor
can call it as `http://neocloud:3000` (see "Address of NeoCloud" below);
then the `aliasgroup1` is `http://neocloud:3000`.

### Reverse proxy

Proxy `office.example.com` to port 9980 with WebSocket support. For nginx:

```nginx
server {
  listen 443 ssl http2;
  server_name office.example.com;
  # certificates ...

  location ^~ /browser { proxy_pass http://127.0.0.1:9980; proxy_set_header Host $http_host; }
  location ^~ /hosting/discovery { proxy_pass http://127.0.0.1:9980; proxy_set_header Host $http_host; }
  location ^~ /hosting/capabilities { proxy_pass http://127.0.0.1:9980; proxy_set_header Host $http_host; }
  location ~ ^/cool/(.*)/ws$ {
    proxy_pass http://127.0.0.1:9980;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
    proxy_set_header Host $http_host;
    proxy_read_timeout 36000s;
  }
  location ~ ^/(c|l)ool { proxy_pass http://127.0.0.1:9980; proxy_set_header Host $http_host; }
  location ^~ /cool/adminws {
    proxy_pass http://127.0.0.1:9980;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
    proxy_set_header Host $http_host;
    proxy_read_timeout 36000s;
  }
}
```

Check that `https://office.example.com/hosting/discovery` shows XML whose
addresses start with `https://office.example.com`. If they show another
host or `http://`, fix `server_name` and `ssl.termination` on the editor.

## 2. Turn it on in NeoCloud

Admin, Server, "Document editor":

| Setting | Meaning |
| --- | --- |
| Editor address | `https://office.example.com` (no path). |
| Address of NeoCloud | Optional. How the *editor* reaches NeoCloud, when that is not how people do (for example `http://neocloud:3000` inside Docker). Empty: the address the person used. |
| Turn on | Saving with this on first asks the editor for its WOPI discovery (`/hosting/discovery`); a wrong address is refused with the reason. |

The same values are stored in the `.env` file as
`NEOCLOUD_OFFICE_EDITOR_URL`, `NEOCLOUD_OFFICE_EDITOR_ENABLED` and
`NEOCLOUD_OFFICE_HOST_URL`. The status line shows whether the editor
answered and which formats it opens; "Check again" asks it again. The
discovery is cached for an hour.

NeoCloud sends the browser a content security policy that allows framing
the editor, and posting to it, from its own address only, and only while the
editor is on. `ALLOWED_ORIGINS` and `NEOCLOUD_CSP_ALLOW_EXTERNAL` are
unaffected.

## How it works

1. A person chooses "Edit" on a document. The app asks
   `POST /api/files/:id/edit-session` and gets the editor's page address plus
   an access token.
2. The app posts the token to the editor page in an iframe. The editor then
   calls NeoCloud's `/wopi/files/...` endpoints with that token (never the
   login cookie).
3. The editor locks the file (30 minutes, refreshed while it is open), reads
   it, and saves it back as a new version. Others who open the same file get
   the same editing session and see each other's changes live.

The token is valid for 10 hours, belongs to one person and one file, and the
person's right is checked again on every call: someone whose share is taken
away, or who only has read access, can no longer save; a file moved to the
trash ends the session. Files in a vault are never opened this way (they are
end-to-end encrypted; the server cannot read them).

Saves are attributed to the person who edited them (the version history
shows who), and are counted against the owner's quota like any upload. The
largest file is the server's maximum upload size.

## Limits and notes

- Locks are kept in memory; after a NeoCloud restart the editor locks the
  file again by itself.
- An editor on plain HTTP cannot be framed from an HTTPS NeoCloud (mixed
  content). Use HTTPS for both.
- On Android and iOS the app cannot embed the editor, so "Edit" opens it in
  the system browser, with the (10 hour, single-file) token in the link.
  Collabora accepts the token as a URL parameter; this path is untested
  against a real editor.
- Other WOPI editors (ONLYOFFICE) that answer `/hosting/discovery` in the
  same format should work but are untested.
