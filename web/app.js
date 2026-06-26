(function() {
'use strict';

const API = '/api';
const EVENTS = '/events';
const FILES = '/files';

let downloads = {};
let sseSource = null;
let connected = false;
let sseConnecting = false;
let renderPending = false;

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

const statusDot = $('#conn-status');
const statusText = $('#conn-text');
const addForm = $('#add-form');
const addUrl = $('#add-url');
const addFilename = $('#add-filename');
const addHeaders = $('#add-headers');
const advancedArea = $('#advanced-area');
const btnToggleAdvanced = $('#btn-toggle-advanced');
const tbody = $('#download-tbody');
const table = $('#download-table');
const empty = $('#list-empty');
const globalRateInput = $('#global-rate');

function setStatus(state, text) {
  statusDot.className = 'dot ' + state;
  statusText.textContent = text;
}

function checkStatus() {
  fetch('/api/status').then(function(r) {
    if (!r.ok) throw new Error(r.statusText);
    return r.json();
  }).then(function(data) {
    if (data.connected) {
      if (!connected) {
        connected = true;
        loadList();
        connectSSE();
      }
      setStatus('connected', 'Connected');
    } else {
      if (connected) {
        connected = false;
        if (sseSource) { sseSource.close(); sseSource = null; }
        downloads = {};
        render();
      }
      setStatus('disconnected', 'Searching for Surge...');
    }
  }).catch(function() {
    if (connected) {
      connected = false;
      if (sseSource) { sseSource.close(); sseSource = null; }
      setStatus('disconnected', 'Connection lost');
      downloads = {};
      render();
    } else {
      setStatus('disconnected', 'Searching for Surge...');
    }
  });
}

function fmtBytes(n) {
  if (n == null || n === 0) return '0 B';
  if (n < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return v.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

function fmtSpeed(bps) {
  return fmtBytes(bps) + '/s';
}

function fmtETA(sec) {
  if (sec == null || sec < 0) return '—';
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.floor(sec/60) + 'm ' + (sec%60) + 's';
  return Math.floor(sec/3600) + 'h ' + Math.floor((sec%3600)/60) + 'm';
}

function toast(msg, type) {
  type = type || '';
  var timer = null;
  const el = document.createElement('div');
  el.className = 'toast ' + type;
  el.textContent = msg;
  document.body.appendChild(el);

  function startTimer() {
    timer = setTimeout(function() { el.remove(); }, 3000);
  }
  el.addEventListener('mouseenter', function() { clearTimeout(timer); });
  el.addEventListener('mouseleave', startTimer);
  startTimer();
}

function api(method, path, body) {
  const opts = { method: method, headers: { 'Content-Type': 'application/json' } };
  if (body) opts.body = JSON.stringify(body);
  return fetch(API + path, opts).then(function(r) {
    return r.text().then(function(t) {
      var data;
      try { data = JSON.parse(t); } catch (_) {}
      if (!r.ok) {
        throw new Error((data && data.message) || t || r.statusText);
      }
      if (data && data.status === 'error') {
        throw new Error(data.message || 'unknown error');
      }
      return data || {};
    });
  });
}

function loadList() {
  return api('GET', '/list').then(function(data) {
    if (!Array.isArray(data)) return;
    data.forEach(function(d) { downloads[d.id] = d; });
    render();
  }).catch(function() {});
}

function connectSSE() {
  if (sseConnecting) return;
  sseConnecting = true;

  function reconnect() {
    if (sseSource) { sseSource.close(); sseSource = null; }
    sseConnecting = false;
    setTimeout(connectSSE, 2000);
  }

  var src = new EventSource(EVENTS);
  sseSource = src;

  src.onopen = function() {
    sseConnecting = false;
  };

  src.addEventListener('progress', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (d.DownloadID && downloads[d.DownloadID]) {
        downloads[d.DownloadID].downloaded = d.Downloaded;
        downloads[d.DownloadID].total_size = d.Total;
        downloads[d.DownloadID].speed = d.Speed;
        downloads[d.DownloadID].eta = d.Downloaded > 0 && d.Speed > 0
          ? Math.round((d.Total - d.Downloaded) / d.Speed) : 0;
        downloads[d.DownloadID].progress = d.Total > 0
          ? (d.Downloaded / d.Total * 100) : 0;
        downloads[d.DownloadID].connections = d.ActiveConnections;
        if (downloads[d.DownloadID].status === 'queued') {
          downloads[d.DownloadID].status = 'downloading';
        }
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('queued', function(e) {
    try {
      var d = JSON.parse(e.data);
      downloads[d.DownloadID] = {
        id: d.DownloadID,
        url: d.URL,
        filename: d.Filename,
        dest_path: d.DestPath,
        total_size: 0,
        downloaded: 0,
        progress: 0,
        speed: 0,
        status: 'queued',
        eta: -1,
        connections: 0,
        error: ''
      };
      render();
    } catch(_) {}
  });

  src.addEventListener('started', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (downloads[d.DownloadID]) {
        downloads[d.DownloadID].status = 'downloading';
        downloads[d.DownloadID].total_size = d.Total;
        downloads[d.DownloadID].filename = d.Filename || downloads[d.DownloadID].filename;
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('complete', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (downloads[d.DownloadID]) {
        downloads[d.DownloadID].status = 'completed';
        downloads[d.DownloadID].progress = 100;
        downloads[d.DownloadID].speed = 0;
        downloads[d.DownloadID].eta = 0;
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('error', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (d.DownloadID && downloads[d.DownloadID]) {
        downloads[d.DownloadID].status = 'error';
        downloads[d.DownloadID].error = d.Err || 'Unknown error';
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('paused', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (d.DownloadID && downloads[d.DownloadID]) {
        downloads[d.DownloadID].status = 'paused';
        downloads[d.DownloadID].speed = 0;
        downloads[d.DownloadID].eta = -1;
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('resumed', function(e) {
    try {
      var d = JSON.parse(e.data);
      if (d.DownloadID && downloads[d.DownloadID]) {
        downloads[d.DownloadID].status = 'downloading';
        render();
      }
    } catch(_) {}
  });

  src.addEventListener('removed', function(e) {
    try {
      var d = JSON.parse(e.data);
      delete downloads[d.DownloadID];
      render();
    } catch(_) {}
  });

  src.onerror = function() {
    src.close();
    reconnect();
  };
}

function render() {
  if (renderPending) return;
  renderPending = true;
  requestAnimationFrame(function() {
    renderPending = false;
    doRender();
  });
}

function doRender() {
  var ids = Object.keys(downloads);
  if (ids.length === 0) {
    table.style.display = 'none';
    empty.style.display = '';
    return;
  }
  table.style.display = '';
  empty.style.display = 'none';

  ids.sort(function(a, b) {
    var sa = downloads[a];
    var sb = downloads[b];
    var pa = (sa.status === 'downloading' || sa.status === 'queued') ? 0 :
             sa.status === 'paused' ? 1 : 2;
    var pb = (sb.status === 'downloading' || sb.status === 'queued') ? 0 :
             sb.status === 'paused' ? 1 : 2;
    if (pa !== pb) return pa - pb;
    return sa.filename && sb.filename ? sa.filename.localeCompare(sb.filename) : 0;
  });

  tbody.innerHTML = ids.map(function(id) {
    var d = downloads[id];
    return rowHTML(id, d);
  }).join('');
}

function rowHTML(id, d) {
  var progress = d.progress || 0;
  var progCls = d.status === 'completed' ? 'completed' :
                d.status === 'error' ? 'error' :
                d.status === 'paused' ? 'paused' : '';

  var statusText = d.status === 'error' ? '<span style="color:var(--red)">' + esc(d.error || 'Error') + '</span>' :
                   d.status === 'queued' ? '⏳ Queued' :
                   d.status === 'paused' ? '⏸ Paused' :
                   d.status === 'completed' ? '✅ Completed' :
                   d.status === 'downloading' ? progress.toFixed(1) + '%' :
                   esc(d.status);

  var speedHTML = d.speed > 0 ? fmtSpeed(d.speed) : '—';
  var etaHTML = d.eta > 0 ? fmtETA(d.eta) : '—';

  var actions = '';
  var escId = esc(id);
  if (d.status === 'downloading') {
    actions += '<button data-action="pause" data-id="' + escId + '">Pause</button>';
  }
  if (d.status === 'paused' || d.status === 'error') {
    actions += '<button data-action="resume" data-id="' + escId + '">Resume</button>';
  }
  if (d.status === 'completed') {
    actions += '<button class="download-btn" data-action="download" data-id="' + escId + '">Download</button>';
  }
  if (d.status === 'completed' || d.status === 'error' || d.status === 'paused') {
    actions += '<button class="danger" data-action="delete" data-id="' + escId + '">Delete</button>';
  }
  if (d.status === 'downloading' || d.status === 'queued') {
    actions += '<button class="danger" data-action="delete" data-id="' + escId + '">Cancel</button>';
  }

  return '<tr>' +
    '<td title="' + esc(d.url || '') + '">' + esc(d.filename || escId) + '</td>' +
    '<td class="size">' + fmtBytes(d.total_size) + '</td>' +
    '<td class="progress-cell">' +
      '<div class="progress-bar"><div class="progress-fill ' + progCls + '" style="width:' + Math.min(100, progress) + '%"></div></div>' +
      '<div class="progress-text">' + statusText + '</div>' +
    '</td>' +
    '<td class="speed">' + speedHTML + '</td>' +
    '<td class="eta">' + etaHTML + '</td>' +
    '<td class="actions">' + actions + '</td>' +
    '</tr>';
}

function esc(s) {
  if (!s) return '';
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

tbody.addEventListener('click', function(e) {
  var btn = e.target.closest('button[data-action]');
  if (!btn) return;
  var action = btn.dataset.action;
  var id = btn.dataset.id;

  switch (action) {
    case 'pause':
      api('POST', '/pause?id=' + encodeURIComponent(id)).then(function() {
        downloads[id].status = 'paused';
        downloads[id].speed = 0;
        render();
      }).catch(function(err) { toast('Pause failed: ' + err.message, 'error'); });
      break;
    case 'resume':
      api('POST', '/resume?id=' + encodeURIComponent(id)).then(function() {
        downloads[id].status = 'downloading';
        render();
      }).catch(function(err) { toast('Resume failed: ' + err.message, 'error'); });
      break;
    case 'delete':
      var purge = confirm('Delete downloaded file as well?');
      api('POST', (purge ? '/purge' : '/delete') + '?id=' + encodeURIComponent(id)).then(function() {
        delete downloads[id];
        render();
      }).catch(function(err) { toast('Delete failed: ' + err.message, 'error'); });
      break;
    case 'download':
      window.open(FILES + '/' + id, '_blank');
      break;
  }
});

function parseHeaders(text) {
  var headers = {};
  if (!text) return headers;
  text.split('\n').forEach(function(line) {
    var idx = line.indexOf(':');
    if (idx > 0) {
      var key = line.substring(0, idx).trim();
      var val = line.substring(idx + 1).trim();
      if (key && val) headers[key] = val;
    }
  });
  return headers;
}

btnToggleAdvanced.addEventListener('click', function() {
  if (advancedArea.style.display === 'none') {
    advancedArea.style.display = '';
    btnToggleAdvanced.textContent = '- Advanced';
  } else {
    advancedArea.style.display = 'none';
    addFilename.value = '';
    addHeaders.value = '';
    btnToggleAdvanced.textContent = '+ Advanced';
  }
});

addForm.addEventListener('submit', function(e) {
  e.preventDefault();
  var url = addUrl.value.trim();
  if (!url) return;

  var body = { url: url };
  var fn = addFilename.value.trim();
  if (fn) body.filename = fn;

  if (advancedArea.style.display !== 'none') {
    var hdrs = parseHeaders(addHeaders.value);
    if (Object.keys(hdrs).length > 0) body.headers = hdrs;
  }

  var btn = addForm.querySelector('button');
  btn.disabled = true;
  btn.textContent = 'Adding...';

  api('POST', '/download', body).then(function() {
    addUrl.value = '';
    addFilename.value = '';
    toast('Download queued', 'success');
  }).catch(function(err) {
    toast(err.message, 'error');
  }).finally(function() {
    btn.disabled = false;
    btn.textContent = 'Add';
  });
});

$('#btn-clear-completed').addEventListener('click', function() {
  api('POST', '/clear-completed').then(function(r) {
    Object.keys(downloads).forEach(function(id) {
      if (downloads[id].status === 'completed') delete downloads[id];
    });
    render();
    toast('Cleared ' + (r.deleted || 0) + ' completed', 'success');
  }).catch(function(err) { toast(err.message, 'error'); });
});

$('#btn-clear-failed').addEventListener('click', function() {
  api('POST', '/clear-failed').then(function(r) {
    Object.keys(downloads).forEach(function(id) {
      if (downloads[id].status === 'error') delete downloads[id];
    });
    render();
    toast('Cleared ' + (r.deleted || 0) + ' failed', 'success');
  }).catch(function(err) { toast(err.message, 'error'); });
});

$('#btn-set-rate').addEventListener('click', function() {
  var rate = parseInt(globalRateInput.value, 10);
  if (isNaN(rate) || rate < 0) rate = 0;
  api('POST', '/rate-limit/global?rate=' + rate).then(function() {
    toast(rate === 0 ? 'Global rate limit removed' : 'Global limit: ' + fmtSpeed(rate), 'success');
  }).catch(function(err) { toast(err.message, 'error'); });
});

checkStatus();
setInterval(checkStatus, 5000);

window.addEventListener('beforeunload', function() {
  if (sseSource) {
    sseSource.close();
    sseSource = null;
  }
});

})();
