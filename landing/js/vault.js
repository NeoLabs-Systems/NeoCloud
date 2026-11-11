/* NeoCloud landing visuals:
   - the hero vault: files arrive, upload and turn into sealed ciphertext
   - the key ladder: segments seal one after another */
(() => {
  const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
  const wait = (ms) => new Promise((resolve) => setTimeout(resolve, reduced ? 0 : ms));
  const HEX = '0123456789abcdef';
  const noise = (n) => Array.from({ length: n }, () => HEX[Math.floor(Math.random() * 16)]).join('');

  /* ---------- hero vault ---------- */
  const vault = document.querySelector('[data-vault]');
  if (vault) {
    const list = vault.querySelector('[data-vault-list]');
    const status = vault.querySelector('[data-vault-status]');
    const meter = vault.querySelector('[data-vault-meter]');
    const batches = [
      [
        { icon: '🖼️', name: 'Lisbon-sunset.heic', size: 4.2 },
        { icon: '🎬', name: 'Birthday-party.mp4', size: 812 },
        { icon: '📄', name: 'Rental-contract.pdf', size: 1.1 },
        { icon: '📊', name: 'Budget-2026.xlsx', size: 0.3 },
      ],
      [
        { icon: '🖼️', name: 'Mountains-panorama.jpg', size: 9.8 },
        { icon: '📝', name: 'Recipes.md', size: 0.02 },
        { icon: '🎵', name: 'Voice-memo-03.m4a', size: 6.4 },
        { icon: '📦', name: 'Website-backup.zip', size: 240 },
      ],
    ];
    let usedGb = 2.4;

    const formatSize = (mb) => (mb >= 1000 ? `${(mb / 1000).toFixed(1)} GB` : mb >= 1 ? `${mb.toFixed(1)} MB` : `${Math.round(mb * 1000)} KB`);

    const row = (file) => {
      const li = document.createElement('li');
      li.className = 'vault-item';
      li.innerHTML =
        `<span class="vault-icon" aria-hidden="true">${file.icon}</span>` +
        `<span class="vault-name"></span>` +
        `<span class="vault-size">${formatSize(file.size)}</span>` +
        '<span class="vault-bytes"><span data-bytes></span><span class="vault-bar"></span></span>';
      li.querySelector('.vault-name').textContent = file.name;
      return li;
    };

    async function upload(li) {
      const bytes = li.querySelector('[data-bytes]');
      const bar = li.querySelector('.vault-bar');
      const steps = reduced ? 1 : 18;
      for (let i = 1; i <= steps; i += 1) {
        bar.style.width = `${(i / steps) * 100}%`;
        bytes.textContent = `${noise(8)} ${noise(8)} ${noise(8)} ${noise(8)} ${noise(8)}`;
        await wait(55);
      }
      li.classList.add('sealed');
      bytes.textContent = `NCB1 ${noise(8)} ${noise(8)} ${noise(8)} ${noise(8)}`;
    }

    async function loop() {
      let round = 0;
      for (;;) {
        const files = batches[round % batches.length];
        list.replaceChildren();
        status.textContent = 'Encrypting in 64 KB segments…';
        const rows = files.map(row);
        rows.forEach((li) => list.appendChild(li));
        for (const li of rows) {
          await wait(120);
          li.classList.add('in');
        }
        for (const [index, li] of rows.entries()) {
          await upload(li);
          usedGb += files[index].size / 1000;
          meter.textContent = `${usedGb.toFixed(1)} GB of 50 GB`;
          await wait(260);
        }
        status.textContent = `${files.length} files sealed · readable only with your keys`;
        round += 1;
        await wait(3400);
      }
    }

    loop();
  }

  /* ---------- key ladder segments ---------- */
  const segments = document.querySelector('[data-segments]');
  if (segments) {
    const cells = Array.from({ length: 16 }, () => {
      const cell = document.createElement('i');
      segments.appendChild(cell);
      return cell;
    });
    let started = false;
    const seal = async () => {
      for (;;) {
        for (const cell of cells) {
          cell.classList.add('sealed');
          await wait(140);
        }
        await wait(2200);
        cells.forEach((cell) => cell.classList.remove('sealed'));
        await wait(600);
      }
    };
    new IntersectionObserver((entries, observer) => {
      if (!started && entries.some((entry) => entry.isIntersecting)) {
        started = true;
        observer.disconnect();
        seal();
      }
    }, { threshold: 0.3 }).observe(segments);
  }
})();
