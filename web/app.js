(function() {
'use strict';

const API = '/api';
const EVENTS = '/events';
const FILES = '/file';

let downloads = {};
let sseSource = null;
let connected = false;
let sseConnecting = false;
let renderPending = false;
let lastStructKey = '';

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

function copyToClipboard(text, label) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(function() {
      toast('Copied ' + label, 'success');
    }).catch(function() {
      legacyCopy(text, label);
    });
  } else {
    legacyCopy(text, label);
  }
}

function legacyCopy(text, label) {
  var ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  try {
    document.execCommand('copy');
    toast('Copied ' + label, 'success');
  } catch (_) {
    ta.select();
    toast('Copy blocked — text selected, press Ctrl+C', 'error');
  }
  document.body.removeChild(ta);
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
    if (Array.isArray(data)) {
      data.forEach(function(d) {
        d.progress = d.status === 'completed' ? 100 : d.progress || 0;
        d.speed = d.status === 'completed' ? 0 : d.speed || 0;
        downloads[d.id] = d;
      });
    }
    return api('GET', '/history');
  }).then(function(data) {
    if (Array.isArray(data)) {
      data.forEach(function(d) {
        if (!downloads[d.id]) {
          d.progress = d.status === 'completed' ? 100 : 0;
          d.speed = 0;
          d.eta = 0;
          d.connections = 0;
          d.error = '';
          downloads[d.id] = d;
        }
      });
    }
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
    if (src.readyState === EventSource.CONNECTING || src.readyState === EventSource.OPEN) return;
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
    lastStructKey = '';
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
    var ta = sa.added_at || sa.completed_at || 0;
    var tb = sb.added_at || sb.completed_at || 0;
    return tb - ta;
  });

  var key = ids.map(function(id) { return id + ':' + downloads[id].status; }).join('|');
  if (key !== lastStructKey) {
    lastStructKey = key;
    tbody.innerHTML = ids.map(function(id) {
      return rowHTML(id, downloads[id]);
    }).join('');
  } else {
    ids.forEach(function(id) {
      var d = downloads[id];
      var row = document.querySelector('tr[data-id="' + CSS.escape(id) + '"]');
      if (!row) return;
      var fill = row.querySelector('.progress-fill');
      if (fill) fill.style.width = Math.min(100, d.progress || 0) + '%';
      var text = row.querySelector('.progress-text');
      if (text && d.status === 'downloading') text.textContent = (d.progress || 0).toFixed(1) + '%';
      var speed = row.querySelector('.speed');
      if (speed) speed.textContent = (d.status !== 'downloading') ? '\u2014' :
                                     d.speed > 0 ? fmtSpeed(d.speed) : '\u2014';
      var eta = row.querySelector('.eta');
      if (eta) eta.textContent = (d.status !== 'downloading') ? '\u2014' :
                                 d.eta > 0 ? fmtETA(d.eta) : '\u2014';
    });
  }
}

function rowHTML(id, d) {
  var progress = d.progress || 0;
  var progCls = d.status === 'completed' ? 'completed' :
                d.status === 'error' ? 'error' :
                d.status === 'paused' ? 'paused' : '';

  var statusText = d.status === 'error' ? '<span style="color:var(--red)">' + esc(d.error || 'Error') + '</span>' :
                   d.status === 'queued' ? '⏳ Queued' :
                   d.status === 'paused' ? '⏸ Paused' :
                   d.status === 'completed' ? 'Completed' :
                   d.status === 'downloading' ? progress.toFixed(1) + '%' :
                   esc(d.status);

  var speedHTML = (d.status === 'completed' || d.status === 'error' || d.status === 'queued') ? '—' :
                  d.speed > 0 ? fmtSpeed(d.speed) : '—';
  var etaHTML = (d.status === 'completed' || d.status === 'error' || d.status === 'queued') ? '—' :
                d.eta > 0 ? fmtETA(d.eta) : '—';

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
    actions += '<button class="encrypt-btn" data-action="encrypt" data-id="' + escId + '">Encrypt</button>';
  }
  if (d.status === 'completed' || d.status === 'error' || d.status === 'paused') {
    actions += '<button class="danger" data-action="delete" data-id="' + escId + '">Delete</button>';
  }
  if (d.status === 'downloading' || d.status === 'queued') {
    actions += '<button class="danger" data-action="delete" data-id="' + escId + '">Cancel</button>';
  }

  return '<tr data-id="' + escId + '">' +
    '<td class="filename-cell" title="' + esc(d.filename || d.url || '') + '" data-filename="' + esc(d.filename || '') + '" data-url="' + esc(d.url || '') + '">' +
      '<div class="filename-wrap"><span class="file-name">' + esc(d.filename || escId) + '</span> <span class="copy-url" title="Copy URL">\u2197</span></div>' +
    '</td>' +
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
  var cell = e.target.closest('.filename-cell');
  if (cell) {
    if (e.target.closest('.copy-url')) {
      var url = cell.dataset.url;
      if (url) {
        copyToClipboard(url, 'URL');
      }
    } else if (cell.dataset.filename) {
      copyToClipboard(cell.dataset.filename, 'filename');
    }
    return;
  }

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
      if (!confirm('Remove this download?')) break;
      var purge = false;
      if (downloads[id].status === 'completed') {
        purge = confirm('Also delete downloaded file?');
      }
      api('POST', (purge ? '/purge' : '/delete') + '?id=' + encodeURIComponent(id)).then(function() {
        delete downloads[id];
        render();
      }).catch(function(err) { toast('Delete failed: ' + err.message, 'error'); });
      break;
    case 'download':
      var a = document.createElement('a');
      a.href = FILES + '/' + id;
      a.download = '';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      break;
    case 'encrypt':
      var pw = prompt('Enter encryption password:');
      if (!pw) break;
      var encUrl = FILES + '/' + id + '/encrypt?password=' + encodeURIComponent(pw);
      var ea = document.createElement('a');
      ea.href = encUrl;
      ea.download = '';
      document.body.appendChild(ea);
      ea.click();
      document.body.removeChild(ea);
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
  var ids = Object.keys(downloads).filter(function(id) {
    return downloads[id].status === 'completed';
  });
  if (ids.length === 0) return;
  if (!confirm('Clear ' + ids.length + ' completed download' + (ids.length !== 1 ? 's' : '') + '?')) return;
  var purgeFiles = confirm('Also delete downloaded files?');
  (purgeFiles ? Promise.all(ids.map(function(id) {
    return api('POST', '/purge?id=' + encodeURIComponent(id));
  })) : api('POST', '/clear-completed')).then(function(r) {
    ids.forEach(function(id) { delete downloads[id]; });
    render();
    var deleted = purgeFiles ? ids.length : (r.deleted || 0);
    toast('Cleared ' + deleted, 'success');
  }).catch(function(err) { toast(err.message, 'error'); });
});

$('#btn-clear-failed').addEventListener('click', function() {
  var ids = Object.keys(downloads).filter(function(id) {
    return downloads[id].status === 'error';
  });
  if (ids.length === 0) return;
  if (!confirm('Clear ' + ids.length + ' failed download' + (ids.length !== 1 ? 's' : '') + '?')) return;
  var purgeFiles = confirm('Also delete downloaded files?');
  (purgeFiles ? Promise.all(ids.map(function(id) {
    return api('POST', '/purge?id=' + encodeURIComponent(id));
  })) : api('POST', '/clear-failed')).then(function(r) {
    ids.forEach(function(id) { delete downloads[id]; });
    render();
    var deleted = purgeFiles ? ids.length : (r.deleted || 0);
    toast('Cleared ' + deleted, 'success');
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

var decryptModal = $('#decrypt-modal');
var decryptClose = $('#decrypt-close');
var decryptDropzone = $('#decrypt-dropzone');
var decryptFileInput = $('#decrypt-file-input');
var decryptDropText = $('#decrypt-drop-text');
var decryptPassword = $('#decrypt-password');
var decryptBtn = $('#decrypt-btn');
var decryptError = $('#decrypt-error');
var decryptSelectedFile = null;

$('#btn-decrypt-tool').addEventListener('click', function() {
  decryptModal.style.display = '';
  decryptError.style.display = 'none';
  decryptPassword.value = '';
  decryptSelectedFile = null;
  decryptDropText.textContent = 'Click to select file or drag here';
});

decryptClose.addEventListener('click', function() {
  decryptModal.style.display = 'none';
});

document.querySelector('.modal-overlay').addEventListener('click', function() {
  decryptModal.style.display = 'none';
});

decryptDropzone.addEventListener('click', function() {
  decryptFileInput.click();
});

decryptFileInput.addEventListener('change', function() {
  if (this.files.length > 0) {
    decryptSelectedFile = this.files[0];
    decryptDropText.textContent = decryptSelectedFile.name;
    decryptError.style.display = 'none';
  }
});

decryptDropzone.addEventListener('dragover', function(e) {
  e.preventDefault();
  this.classList.add('dragover');
});

decryptDropzone.addEventListener('dragleave', function() {
  this.classList.remove('dragover');
});

decryptDropzone.addEventListener('drop', function(e) {
  e.preventDefault();
  this.classList.remove('dragover');
  if (e.dataTransfer.files.length > 0) {
    decryptSelectedFile = e.dataTransfer.files[0];
    decryptDropText.textContent = decryptSelectedFile.name;
    decryptError.style.display = 'none';
  }
});

decryptBtn.addEventListener('click', function() {
  if (!decryptSelectedFile) {
    decryptError.textContent = 'Please select a .enc file.';
    decryptError.style.display = '';
    return;
  }
  var pw = decryptPassword.value;
  if (!pw) {
    decryptError.textContent = 'Please enter the password.';
    decryptError.style.display = '';
    return;
  }

  if (!window.crypto || !crypto.subtle) {
    decryptError.textContent = 'Web Crypto API not available. Use HTTPS or open via localhost.';
    decryptError.style.display = '';
    return;
  }

  decryptBtn.disabled = true;
  decryptBtn.textContent = 'Reading header...';
  decryptError.style.display = 'none';

  var file = decryptSelectedFile;
  var headerSize = 21;

  if (file.size < headerSize) {
    decryptError.textContent = 'Invalid .enc file format.';
    decryptError.style.display = '';
    decryptBtn.disabled = false;
    decryptBtn.textContent = 'Decrypt & Download';
    return;
  }

  file.slice(0, headerSize).arrayBuffer().then(function(headerBuf) {
    var header = new Uint8Array(headerBuf);
    if (String.fromCharCode.apply(null, header.slice(0, 4)) !== 'SENC' || header[4] !== 0x02) {
      throw new Error('Invalid format');
    }
    var nonce = header.slice(5, 21);
    return crypto.subtle.digest('SHA-256', new TextEncoder().encode('enc' + pw))
      .then(function(kh) { return crypto.subtle.importKey('raw', kh, {name: 'AES-CTR'}, false, ['decrypt']); })
      .then(function(encKey) { return {encKey: encKey, nonce: nonce}; });
  }).then(function(state) {
    var encKey = state.encKey;
    var nonce = state.nonce;
    var cipherSize = file.size - headerSize;
    var stream = file.slice(headerSize).stream();
    var reader = stream.getReader();
    var bytesRead = 0;
    var nameParsed = false;
    var filename = '';
    var writable = null;
    var blobChunks = [];

    function makeCounter(base, blockOffset) {
      var n = 0n;
      for (var i = 0; i < base.length; i++) n = (n << 8n) | BigInt(base[i]);
      n += BigInt(blockOffset);
      var r = new Uint8Array(16);
      for (var i = 15; i >= 0; i--) { r[i] = Number(n & 0xFFn); n >>= 8n; }
      return r;
    }

    function process() {
      return reader.read().then(function(result) {
        if (result.done) return null;
        var chunk = new Uint8Array(result.value);
        var blockOffset = bytesRead / 16;
        bytesRead += chunk.length;
        return crypto.subtle.decrypt(
          {name: 'AES-CTR', counter: makeCounter(nonce, blockOffset), length: 128},
          encKey, chunk
        ).then(function(decrypted) {
          var plain = new Uint8Array(decrypted);
          var pct = Math.round(bytesRead / cipherSize * 100);

          if (!nameParsed) {
            var nameLen = new DataView(plain.buffer, plain.byteOffset, 4).getUint32(0, true);
            if (nameLen > 255) throw new Error('Wrong password or corrupted file');
            filename = new TextDecoder().decode(plain.slice(4, 4 + nameLen));
            nameParsed = true;
            var contentStart = 4 + nameLen;
            if (window.showSaveFilePicker) {
              decryptBtn.textContent = 'Saving...';
              return window.showSaveFilePicker({suggestedName: filename}).then(function(handle) {
                return handle.createWritable().then(function(w) {
                  writable = w;
                  if (contentStart < plain.length) {
                    decryptBtn.textContent = 'Saving... ' + pct + '%';
                    return writable.write(plain.slice(contentStart)).then(function() { return process(); });
                  }
                  return process();
                });
              });
            }
            if (contentStart < plain.length) blobChunks.push(plain.slice(contentStart));
            decryptBtn.textContent = 'Decrypting... ' + pct + '%';
            return process();
          }

          if (writable) {
            decryptBtn.textContent = 'Saving... ' + pct + '%';
            return writable.write(plain).then(function() { return process(); });
          }
          blobChunks.push(plain);
          decryptBtn.textContent = 'Decrypting... ' + pct + '%';
          return process();
        });
      });
    }

    decryptBtn.textContent = 'Decrypting...';
    return process().then(function() {
      if (writable) return writable.close();
      var blob = new Blob(blobChunks);
      var url = URL.createObjectURL(blob);
      var a = document.createElement('a');
      a.href = url; a.download = filename;
      document.body.appendChild(a); a.click(); document.body.removeChild(a);
      URL.revokeObjectURL(url);
    });
  }).then(function() {
    decryptModal.style.display = 'none';
  }).catch(function(e) {
    if (e.name === 'AbortError') return;
    decryptError.textContent = 'Decryption failed. Wrong password or corrupted file.';
    decryptError.style.display = '';
  }).finally(function() {
    decryptBtn.disabled = false;
    decryptBtn.textContent = 'Decrypt & Download';
  });
});

})();
