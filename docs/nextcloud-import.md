# Move from Nextcloud

NeoCloud copies your Nextcloud files over by itself, folders, modification
dates and favorites included.

1. In Nextcloud, open **Settings › Security**, enter a name like
   "NeoCloud import" under *Devices & sessions* and click **Create new app
   password**. Copy the password it shows. Use an app password, not your
   login password: it also works with two-factor sign-in, and you can revoke
   it when the move is done.
2. In NeoCloud open **Settings › Import**, enter your Nextcloud address
   (`https://cloud.example.com`), your user name and that app password, and
   test the connection.
3. Start the import. Files land in a folder `Nextcloud` (or a folder you
   pick). The import runs in the background, survives restarts and can be
   paused and resumed. Running it again later only fetches what is new or
   changed.

The password is stored encrypted while the import runs and deleted when it
ends. Share links, shares with other users, versions, trash, comments, tags
and end-to-end encrypted folders stay in Nextcloud; only your current files
and folders move. Any other WebDAV server works too.

Only admins can import from a server on the local network (or set
`NEOCLOUD_IMPORT_ALLOW_PRIVATE=true`). The API is described under
[Imports](api.md#imports--apiimports-session-or-phone).
