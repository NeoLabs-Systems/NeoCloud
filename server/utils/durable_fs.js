'use strict';

// fsync helpers. Writing a file and renaming it into place survives a power
// cut only when the file's bytes and then the directory entry naming it have
// both reached the disk.

const fs = require('fs');

async function syncFile(filePath) {
  const handle = await fs.promises.open(filePath, 'r+');
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

// Windows cannot open a directory for fsync; NTFS journals renames itself.
async function syncDirectory(directory) {
  if (process.platform === 'win32') return;
  const handle = await fs.promises.open(directory, 'r');
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

module.exports = { syncDirectory, syncFile };
