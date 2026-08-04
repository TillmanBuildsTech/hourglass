
let jobs = [];
let deleteIndex = -1;
let editIndex = -1;
let isLocalOnly = false;
let currentConnection = { host: 'localhost', port: 8080 };
let activeConnectionId = '';

// ── Theme ──────────────────────────────────────────────────────────────────
(function initTheme() {
    const saved = localStorage.getItem('hg-theme');
    if (saved) document.documentElement.dataset.theme = saved;
    syncThemeIcon();
})();

function syncThemeIcon() {
    const isDark = document.documentElement.dataset.theme === 'dark'
        || (!document.documentElement.dataset.theme && window.matchMedia('(prefers-color-scheme: dark)').matches);
    document.getElementById('icon-sun').style.display  = isDark  ? 'none'  : '';
    document.getElementById('icon-moon').style.display = isDark  ? '' : 'none';
}

document.getElementById('theme-toggle').addEventListener('click', () => {
    const root = document.documentElement;
    const isDark = root.dataset.theme === 'dark'
        || (!root.dataset.theme && window.matchMedia('(prefers-color-scheme: dark)').matches);
    root.dataset.theme = isDark ? 'light' : 'dark';
    localStorage.setItem('hg-theme', root.dataset.theme);
    syncThemeIcon();
});

// ── Notifications ──────────────────────────────────────────────────────────
function showNotification(msg, type) {
    const banner = document.getElementById('error-banner');
    const text   = document.getElementById('error-text');
    text.textContent = msg;
    banner.className = `alert ${type}`;
    banner.classList.remove('hidden');
    if (type !== 'error') {
        clearTimeout(banner._timer);
        banner._timer = setTimeout(clearError, 4000);
    }
}

function showError(msg) {
    const isSuccess = msg.startsWith('✓') || msg.startsWith('Connected') || msg.startsWith('Switched');
    showNotification(msg, isSuccess ? 'success' : 'error');
}

function clearError() {
    document.getElementById('error-banner').classList.add('hidden');
}

// ── Connection Management ──────────────────────────────────────────────────
function initConnections() {
    detectLocalBinding();
    loadConnections();
}

function detectLocalBinding() {
    const host = window.location.hostname;
    isLocalOnly = host === 'localhost' || host === '127.0.0.1';

    if (isLocalOnly) {
        document.getElementById('local-only-message').classList.remove('hidden');
        document.getElementById('add-conn-btn').style.display = 'none';
    } else {
        document.getElementById('local-only-message').classList.add('hidden');
        document.getElementById('add-conn-btn').style.display = '';
    }
}

async function loadConnections() {
    try {
        const resp = await fetch('/api/connections');
        if (!resp.ok) return;

        const data = await resp.json();
        activeConnectionId = data.active_id || '';
        const connections = data.connections || [];

        if (connections.length > 0) {
            document.getElementById('saved-connections').classList.remove('hidden');
            const items = document.getElementById('connections-items');
            items.innerHTML = connections.map((conn) => {
                const isActive = conn.id === activeConnectionId;
                const label    = escapeHtml(conn.label || conn.host);
                const detail   = `${escapeHtml(conn.user)}@${escapeHtml(conn.host)}:${conn.port}`;
                return `
                <div class="conn-card ${isActive ? 'active' : ''}">
                    <div class="conn-icon">
                        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                            <rect x="2" y="2" width="20" height="8" rx="2"/>
                            <rect x="2" y="14" width="20" height="8" rx="2"/>
                            <path d="M6 6h.01M6 18h.01"/>
                        </svg>
                    </div>
                    <div class="conn-info">
                        <div class="conn-name">${label}</div>
                        <div class="conn-detail">${detail}</div>
                    </div>
                    <div style="display:flex;align-items:center;gap:4px">
                        ${isActive
                            ? '<span class="conn-status-dot"></span>'
                            : `<button onclick="switchConnection('${escapeHtml(conn.id)}')" class="sidebar-btn" style="padding:3px 6px;font-size:10px;width:auto">Connect</button>`
                        }
                        <button onclick="removeConnection('${escapeHtml(conn.id)}')" title="Remove" style="background:none;border:none;cursor:pointer;color:var(--text-3);padding:2px;line-height:1;font-size:14px" onmouseover="this.style.color='var(--red)'" onmouseout="this.style.color='var(--text-3)'">×</button>
                    </div>
                </div>`;
            }).join('');
        } else {
            document.getElementById('saved-connections').classList.add('hidden');
        }

        updateConnectionDisplay(connections);
    } catch (err) {
        console.error('Failed to load connections:', err);
    }
}

function updateConnectionDisplay(connections) {
    const active = (connections || []).find((c) => c.id === activeConnectionId);
    const label  = active ? (active.label || active.host) : 'Local';
    const detail = active ? `${active.user}@${active.host}:${active.port}` : 'localhost';

    document.getElementById('connection-status').textContent  = label;
    document.getElementById('current-conn-display').textContent = detail;
    document.getElementById('switch-local-btn').classList.toggle('hidden', !active);

    // Mark local card active/inactive
    const localCard = document.getElementById('current-connection');
    localCard.classList.toggle('active', !active);
}

async function switchConnection(connId) {
    try {
        const resp = await fetch('/api/connections/active', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: connId })
        });

        if (!resp.ok) {
            const err = await resp.json();
            showError('Failed to switch connection: ' + err.error);
            return;
        }

        document.getElementById('connections-panel').classList.remove('hidden');
        await loadConnections();
        await loadJobs();
        showError(connId ? 'Connected to ' + connId : 'Switched to Local');
    } catch (err) {
        showError('Failed to switch connection: ' + err.message);
    }
}

async function removeConnection(connId) {
    try {
        const resp = await fetch('/api/connections', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id: connId })
        });

        if (!resp.ok) throw new Error('Failed to delete connection');
        await loadConnections();
    } catch (err) {
        showError('Failed to remove connection: ' + err.message);
    }
}

async function testConnection() {
    const host    = document.getElementById('conn-host').value.trim();
    const port    = parseInt(document.getElementById('conn-port').value) || 22;
    const user    = document.getElementById('conn-user').value.trim();
    const keyPath = document.getElementById('conn-keypath').value.trim();
    const statusEl = document.getElementById('conn-test-status');

    if (!host || !user || !keyPath) {
        statusEl.textContent = '✗ Fill in all required fields';
        statusEl.style.color = 'var(--red)';
        return;
    }

    statusEl.textContent = 'Testing…';
    statusEl.style.color = 'var(--text-2)';

    try {
        const resp = await fetch('/api/connections/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ host, port, user, key_path: keyPath })
        });

        if (!resp.ok) {
            const err = await resp.json();
            statusEl.textContent = '✗ ' + err.error;
            statusEl.style.color = 'var(--red)';
            return;
        }

        statusEl.textContent = '✓ Connection successful';
        statusEl.style.color = 'var(--green)';
    } catch (err) {
        statusEl.textContent = '✗ ' + err.message;
        statusEl.style.color = 'var(--red)';
    }
}

async function saveConnection() {
    if (isLocalOnly) {
        showError('Cannot add remote connections when running locally');
        return;
    }

    const host    = document.getElementById('conn-host').value.trim();
    const port    = parseInt(document.getElementById('conn-port').value) || 22;
    const user    = document.getElementById('conn-user').value.trim();
    const keyPath = document.getElementById('conn-keypath').value.trim();
    const label   = document.getElementById('conn-label').value.trim();

    if (!host || !user || !keyPath) {
        showError('Hostname, username, and key path are required');
        return;
    }

    const id = `${host}-${user}-${Date.now()}`;

    try {
        const resp = await fetch('/api/connections', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, host, port, user, key_path: keyPath, label })
        });

        if (!resp.ok) {
            const err = await resp.json();
            showError('Failed to save connection: ' + err.error);
            return;
        }

        ['conn-host','conn-user','conn-keypath','conn-label'].forEach(id => {
            document.getElementById(id).value = '';
        });
        document.getElementById('conn-port').value = '22';
        document.getElementById('conn-test-status').textContent = '';
        document.getElementById('add-connection-form').classList.add('hidden');
        document.getElementById('add-conn-btn').style.display = '';

        await loadConnections();
    } catch (err) {
        showError('Failed to save connection: ' + err.message);
    }
}

// Connection UI wiring
document.getElementById('connections-toggle').addEventListener('click', () => {
    document.getElementById('connections-panel').classList.toggle('hidden');
});
document.getElementById('connections-close').addEventListener('click', () => {
    document.getElementById('connections-panel').classList.add('hidden');
});
document.getElementById('add-conn-btn').addEventListener('click', () => {
    document.getElementById('add-connection-form').classList.remove('hidden');
    document.getElementById('add-conn-btn').style.display = 'none';
});
document.getElementById('conn-test').addEventListener('click', testConnection);
document.getElementById('conn-save').addEventListener('click', saveConnection);
document.getElementById('conn-cancel').addEventListener('click', () => {
    document.getElementById('add-connection-form').classList.add('hidden');
    document.getElementById('add-conn-btn').style.display = '';
    document.getElementById('conn-test-status').textContent = '';
});

// ── Jobs Management ────────────────────────────────────────────────────────
async function loadJobs() {
    try {
        const resp = await fetch('/api/cron');
        if (!resp.ok) throw new Error('Failed to load jobs');
        jobs = await resp.json() || [];
        renderJobs();
        clearError();
    } catch (err) {
        showError('Failed to load cron jobs: ' + err.message);
    }
}

function formatRelativeTime(timestamp) {
    if (!timestamp) return 'Never';
    const diff    = Date.now() - timestamp;
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours   = Math.floor(minutes / 60);
    const days    = Math.floor(hours / 24);
    if (seconds < 60)  return `${seconds}s ago`;
    if (minutes < 60)  return `${minutes}m ago`;
    if (hours   < 24)  return `${hours}h ago`;
    return `${days}d ago`;
}

function escapeHtml(str) {
    return String(str == null ? '' : str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

const ICONS = {
    run:    '<svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14"><path d="M6.5 4.6c0-.9 1-1.5 1.8-1l7.6 4.9a1.2 1.2 0 0 1 0 2l-7.6 5c-.8.5-1.8-.1-1.8-1V4.6Z"/></svg>',
    pause:  '<svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14"><path d="M6 4.5a1 1 0 0 1 1-1h1.5a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-11Zm5.5 0a1 1 0 0 1 1-1H14a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1h-1.5a1 1 0 0 1-1-1v-11Z"/></svg>',
    edit:   '<svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14"><path d="M13.6 2.8a1.6 1.6 0 0 1 2.3 0l1.3 1.3a1.6 1.6 0 0 1 0 2.3L16 7.6 12.4 4l1.2-1.2ZM11 5.4 3 13.4V17h3.6l8-8L11 5.4Z"/></svg>',
    delete: '<svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14"><path fill-rule="evenodd" d="M8 2a1 1 0 0 0-1 1v1H4a1 1 0 1 0 0 2h.4l.7 10.1A2 2 0 0 0 7.1 18h5.8a2 2 0 0 0 2-1.9L15.6 6h.4a1 1 0 1 0 0-2h-3V3a1 1 0 0 0-1-1H8Zm1 2h2V4H9v0Zm-1.6 4a.8.8 0 0 1 1.6 0v6a.8.8 0 0 1-1.6 0V8Zm4.8 0a.8.8 0 0 1 1.6 0v6a.8.8 0 0 1-1.6 0V8Z" clip-rule="evenodd"/></svg>',
};

function iconBtn(icon, onclick, title, extraClass) {
    return `<button onclick="${onclick}" class="btn-icon ${extraClass}" title="${title}" aria-label="${title}">${ICONS[icon]}</button>`;
}

function renderJobs() {
    const tbody = document.getElementById('jobs-table');
    if (jobs.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" class="empty-cell">No cron jobs yet.</td></tr>';
        return;
    }

    tbody.innerHTML = jobs.map((job, idx) => {
        const lastRun    = formatRelativeTime(job.LastRun);
        const hasRun     = !!job.LastRun;
        const isSuccess  = job.LastStatus === 'success';
        const statusCls  = hasRun ? (isSuccess ? 'ok' : 'fail') : 'none';
        const statusIcon = hasRun ? (isSuccess ? '✓' : '✗') : '—';
        const title      = job.Comment || job.Command;
        const subtitle   = job.Comment ? job.Command : '';

        return `
        <tr class="job-row${job.Inactive ? ' inactive' : ''}">
            <td class="job-name-cell">
                <div class="job-title" title="${escapeHtml(title)}">${escapeHtml(title)}</div>
                ${subtitle ? `<div class="job-sub" title="${escapeHtml(subtitle)}">${escapeHtml(subtitle)}</div>` : ''}
                <code class="job-sched">${escapeHtml(job.Schedule)}</code>
            </td>
            <td class="job-lastrun">${lastRun}</td>
            <td class="job-status">
                <span class="status-badge ${statusCls}">${statusIcon}</span>
            </td>
            <td class="job-actions-cell">
                <div class="action-group">
                    ${!job.Inactive ? iconBtn('run',  `executeJob(${idx})`, 'Run now', 'run') : ''}
                    ${iconBtn(job.Inactive ? 'run' : 'pause', `toggleJob(${idx})`, job.Inactive ? 'Enable' : 'Disable', 'toggle')}
                    ${iconBtn('edit',   `editJob(${idx})`,   'Edit',   'edit')}
                    ${iconBtn('delete', `deleteJob(${idx})`, 'Delete', 'del')}
                </div>
            </td>
        </tr>`;
    }).join('');
}

function editJob(idx) {
    editIndex = idx;
    const job = jobs[idx];
    document.getElementById('schedule-input').value = job.Schedule;
    document.getElementById('command-input').value  = job.Command;
    document.getElementById('comment-input').value  = job.Comment || '';
    document.getElementById('form-title').textContent  = 'Update Job';
    document.getElementById('submit-btn').textContent  = 'Update Job';
    document.getElementById('cancel-edit-btn').classList.remove('hidden');
    document.getElementById('schedule-input').focus();
}

function cancelEdit() {
    editIndex = -1;
    document.getElementById('add-job-form').reset();
    document.getElementById('form-title').textContent  = 'Add New Job';
    document.getElementById('submit-btn').textContent  = 'Add Job';
    document.getElementById('cancel-edit-btn').classList.add('hidden');
}

function deleteJob(idx) {
    deleteIndex = idx;
    document.getElementById('delete-modal').classList.remove('hidden');
}

async function executeJob(idx) {
    try {
        const resp = await fetch('/api/cron/execute', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index: idx })
        });

        if (!resp.ok) {
            const err = await resp.json();
            showError('Failed to execute job: ' + err.error);
            return;
        }

        const data = await resp.json();
        showError(`✓ Job executed: ${data.output ? data.output.substring(0, 100) : 'completed'}`);
        setTimeout(loadJobs, 2000);
    } catch (err) {
        showError('Failed to execute job: ' + err.message);
    }
}

async function toggleJob(idx) {
    try {
        const resp = await fetch('/api/cron/toggle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index: idx })
        });

        if (!resp.ok) {
            const err = await resp.json();
            showError('Failed to toggle job: ' + err.error);
            return;
        }

        await loadJobs();
    } catch (err) {
        showError('Failed to toggle job: ' + err.message);
    }
}

document.getElementById('cancel-edit-btn').addEventListener('click', (e) => {
    e.preventDefault();
    cancelEdit();
});

document.getElementById('delete-cancel').addEventListener('click', () => {
    deleteIndex = -1;
    document.getElementById('delete-modal').classList.add('hidden');
});

document.getElementById('delete-confirm').addEventListener('click', async () => {
    if (deleteIndex < 0) return;
    try {
        const resp = await fetch('/api/cron', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ index: deleteIndex })
        });

        if (!resp.ok) throw new Error('Failed to delete job');
        deleteIndex = -1;
        document.getElementById('delete-modal').classList.add('hidden');
        await loadJobs();
    } catch (err) {
        showError('Failed to delete job: ' + err.message);
    }
});

document.getElementById('add-job-form').addEventListener('submit', async (e) => {
    e.preventDefault();

    const schedule = document.getElementById('schedule-input').value;
    const command  = document.getElementById('command-input').value;
    const comment  = document.getElementById('comment-input').value;

    try {
        if (editIndex >= 0) {
            const resp = await fetch('/api/cron/update', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ index: editIndex, Schedule: schedule, Command: command, Comment: comment })
            });
            if (!resp.ok) {
                const err = await resp.json();
                throw new Error(err.error || 'Failed to update job');
            }
        } else {
            const resp = await fetch('/api/cron', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ Schedule: schedule, Command: command, Comment: comment })
            });
            if (!resp.ok) {
                const err = await resp.json();
                throw new Error(err.error || 'Failed to add job');
            }
        }

        document.getElementById('add-job-form').reset();
        cancelEdit();
        await loadJobs();
    } catch (err) {
        showError('Failed to save job: ' + err.message);
    }
});

// ── Version / macOS banner ─────────────────────────────────────────────────
async function initVersion() {
    try {
        const resp = await fetch('/api/version');
        if (!resp.ok) return;
        const data = await resp.json();
        document.getElementById('app-version').textContent = data.version ? `v${data.version}` : '';

        if (data.goos === 'darwin' && !sessionStorage.getItem('hourglass-macos-banner-dismissed')) {
            document.getElementById('macos-banner').classList.remove('hidden');
        }
    } catch (err) {
        console.error('Failed to load version:', err);
    }
}

document.getElementById('macos-banner-dismiss').addEventListener('click', () => {
    document.getElementById('macos-banner').classList.add('hidden');
    sessionStorage.setItem('hourglass-macos-banner-dismissed', '1');
});

// ── Boot ──────────────────────────────────────────────────────────────────
initVersion();
initConnections();
loadJobs();
setInterval(loadJobs, 30000);
