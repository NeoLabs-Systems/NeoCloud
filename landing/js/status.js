(function () {
  'use strict';
  const labels = { operational: 'Operational', degraded: 'Degraded', unavailable: 'Unavailable', unknown: 'Unknown' };
  const titles = { operational: 'All monitored systems operational', degraded: 'Some systems need attention', unavailable: 'Service disruption', unknown: 'Status unavailable' };
  function node(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text) element.textContent = text;
    return element;
  }
  function safeStatus(value) { return Object.hasOwn(labels, value) ? value : 'unknown'; }
  function render(data) {
    const status = safeStatus(data.status);
    document.getElementById('status-icon').className = 'status-icon ' + status;
    document.getElementById('overall').textContent = titles[status];
    document.getElementById('note').textContent = data.note || '';
    document.getElementById('updated').textContent = data.updatedAt ? new Date(data.updatedAt).toLocaleString() : 'Unavailable';
    const items = Array.isArray(data.components) ? data.components : [];
    const list = document.getElementById('components');
    list.replaceChildren();
    items.forEach(function (item) {
      const state = safeStatus(item.status);
      const row = node('article', 'component');
      const main = node('div', 'component-main');
      const details = node('div');
      details.append(node('h3', '', item.name), node('p', '', item.description));
      main.append(node('span', 'component-dot ' + state), details);
      row.append(main, node('span', 'badge ' + state, labels[state]));
      list.append(row);
    });
  }

  async function refresh() {
    try {
      const response = await fetch('/api/health', { cache: 'no-store' });
      if (!response.ok && response.status !== 503) throw new Error('Status unavailable');
      const { status, db, storage } = await response.json();
      if (!['ok', 'degraded'].includes(status) || ![db, storage].every((state) => ['ok', 'error'].includes(state))) {
        throw new Error('Invalid health response');
      }

      render({
        status: status === 'ok' ? 'operational' : 'degraded',
        updatedAt: new Date().toISOString(),
        note: status === 'ok' ? 'All monitored services are operational.' : 'One or more monitored services need attention.',
        components: [
          { name: 'Web server', status: 'operational', description: 'The health endpoint is responding.' },
          {
            name: 'Database',
            status: db === 'ok' ? 'operational' : 'unavailable',
            description: db === 'ok' ? 'The database is responding.' : 'The database check failed.',
          },
          {
            name: 'File storage',
            status: storage === 'ok' ? 'operational' : 'unavailable',
            description: storage === 'ok' ? 'The storage directories are writable.' : 'The storage write-access check failed.',
          },
        ],
      });
    } catch {
      render({ status: 'unknown', note: 'Could not read the health endpoint.' });
    } finally {
      setTimeout(refresh, 60_000);
    }
  }

  refresh();
}());
