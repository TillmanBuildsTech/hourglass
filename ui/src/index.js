
let jobs = [];
let deleteIndex = -1;
let editIndex = -1;
let isLocalOnly = false;
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
    document.getElementById('icon-sun').style.display  = isDark ? 'none' : '';
    document.getElementById('icon-moon').style.display = isDark ? ''     : 'none';
}

document.getElementById('theme-toggle').addEventListener('click', () => {
    const root = document.documentElement;
    const isDark = root.dataset.theme === 'dark'
        || (!root.dataset.theme && window.matchMedia('(prefers-color-scheme: dark)').matches);
    root.dataset.theme = isDark ? 'light' : 'dark';
    localStorage.setItem('hg-theme', root.dataset.theme);
    syncThemeIcon();
});

// ── View Switching ─────────────────────────────────────────────────────────
function switchView(view) {
    document.querySelectorAll('.content-view').forEach(el => el.classList.add('hidden'));
    const target = document.getElementById('view-' + view);
    if (target) target.classList.remove('hidden');
    closeSidebar(); // close mobile drawer when navigating
    closeCronPopup();
}

// ── Mobile Sidebar ─────────────────────────────────────────────────────────
function openSidebar() {
    document.getElementById('connections-panel').classList.add('open');
    document.getElementById('sidebar-backdrop').classList.add('visible');
    document.body.style.overflow = 'hidden';
}
function closeSidebar() {
    document.getElementById('connections-panel').classList.remove('open');
    document.getElementById('sidebar-backdrop').classList.remove('visible');
    document.body.style.overflow = '';
}

document.getElementById('sidebar-hamburger').addEventListener('click', openSidebar);
document.getElementById('sidebar-backdrop').addEventListener('click', closeSidebar);
document.getElementById('sidebar-close').addEventListener('click', closeSidebar);

// Legacy compat — keep hidden elements wired so JS doesn't error
document.getElementById('connections-toggle').addEventListener('click', openSidebar);
document.getElementById('connections-close').addEventListener('click', closeSidebar);

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
    const isOk = msg.startsWith('✓') || msg.startsWith('Connected') || msg.startsWith('Switched');
    showNotification(msg, isOk ? 'success' : 'error');
}

function clearError() {
    document.getElementById('error-banner').classList.add('hidden');
}

// ── Cron Help Popup ────────────────────────────────────────────────────────
function openCronPopup() {
    document.getElementById('cron-popup').classList.remove('hidden');
    document.getElementById('cron-help-btn').classList.add('active');
    updateCronPreview();
}
function closeCronPopup() {
    document.getElementById('cron-popup').classList.add('hidden');
    document.getElementById('cron-help-btn').classList.remove('active');
}
function toggleCronPopup() {
    const popup = document.getElementById('cron-popup');
    popup.classList.contains('hidden') ? openCronPopup() : closeCronPopup();
}

document.getElementById('cron-help-btn').addEventListener('click', (e) => {
    e.stopPropagation();
    toggleCronPopup();
});

// Close popup when clicking outside
document.addEventListener('click', (e) => {
    const popup  = document.getElementById('cron-popup');
    const btn    = document.getElementById('cron-help-btn');
    const input  = document.getElementById('schedule-input');
    if (!popup.classList.contains('hidden') &&
        !popup.contains(e.target) && e.target !== btn && e.target !== input) {
        closeCronPopup();
    }
});

// Live cron preview
function describeCron(expr) {
    const parts = (expr || '').trim().split(/\s+/);
    if (parts.length !== 5) return null;
    const [min, hour, dom, month, dow] = parts;
    const anyOf = s => s === '*' || s === '?';

    // Full wildcard
    if (parts.every(anyOf)) return 'Runs every minute';

    // Step patterns on minute
    if (min.startsWith('*/') && parts.slice(1).every(anyOf)) {
        const n = parseInt(min.slice(2));
        return n > 0 ? `Runs every ${n} minute${n > 1 ? 's' : ''}` : null;
    }

    // Step patterns on hour
    if (anyOf(min) && hour.startsWith('*/') && anyOf(dom) && anyOf(month) && anyOf(dow)) {
        const n = parseInt(hour.slice(2));
        return n > 0 ? `Runs every ${n} hour${n > 1 ? 's' : ''}, on the minute` : null;
    }

    // Build human description
    const dowNames = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];
    const monthNames = ['','Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];

    let parts_desc = [];

    // Weekday
    if (!anyOf(dow)) {
        if (dow === '1-5') parts_desc.push('weekdays');
        else if (dow === '0,6' || dow === '6,0' || dow === '0-6') parts_desc.push('weekends');
        else {
            const days = dow.split(',').map(d => {
                if (d.includes('-')) {
                    const [a, b] = d.split('-');
                    return `${dowNames[+a]}–${dowNames[+b]}`;
                }
                return dowNames[+d] || d;
            });
            parts_desc.push(days.join(', '));
        }
    }

    // Day of month
    if (!anyOf(dom)) {
        const d = parseInt(dom);
        const sfx = [,'st','nd','rd'][d] || 'th';
        parts_desc.push(`on the ${d}${sfx}`);
    }

    // Month
    if (!anyOf(month)) {
        const m = monthNames[parseInt(month)];
        parts_desc.push(`in ${m || month}`);
    }

    // Time
    let time = '';
    if (anyOf(hour) && anyOf(min)) {
        time = 'every minute';
    } else if (anyOf(hour)) {
        if (min.startsWith('*/')) {
            const n = parseInt(min.slice(2));
            time = `every ${n} min`;
        } else {
            time = `at minute :${min.padStart(2,'0')} of each hour`;
        }
    } else if (anyOf(min)) {
        time = `every minute past ${hour}:00`;
    } else {
        const h = parseInt(hour), m = parseInt(min);
        if (isNaN(h) || isNaN(m)) return null;
        const ampm = h >= 12 ? 'PM' : 'AM';
        const h12  = h === 0 ? 12 : h > 12 ? h - 12 : h;
        time = `at ${h12}:${String(m).padStart(2,'0')} ${ampm}`;
    }

    const prefix = parts_desc.length ? parts_desc.join(', ') + ' ' : '';
    return `Runs ${prefix}${time}`;
}

function updateCronPreview() {
    const val = document.getElementById('schedule-input').value.trim();
    const el  = document.getElementById('cron-preview');
    if (!val) {
        el.textContent = 'Enter a schedule above';
        el.className = 'cron-preview-text muted';
        return;
    }
    const desc = describeCron(val);
    if (desc) {
        el.textContent = desc;
        el.className = 'cron-preview-text';
    } else {
        el.textContent = 'Invalid or complex expression';
        el.className = 'cron-preview-text muted';
    }
}

document.getElementById('schedule-input').addEventListener('input', () => {
    if (!document.getElementById('cron-popup').classList.contains('hidden')) {
        updateCronPreview();
    }
});

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
        const connections  = data.connections || [];

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
                    <div style="display:flex;align-items:center;gap:4px;flex-shrink:0">
                        ${isActive
                            ? '<span class="conn-status-dot"></span>'
                            : `<button onclick="switchConnection('${escapeHtml(conn.id)}')" class="sidebar-btn" style="padding:3px 7px;font-size:10px;width:auto">Connect</button>`
                        }
                        <button onclick="removeConnection('${escapeHtml(conn.id)}')" title="Remove"
                            style="background:none;border:none;cursor:pointer;color:var(--text-3);padding:2px 3px;line-height:1;font-size:15px"
                            onmouseover="this.style.color='var(--red)'" onmouseout="this.style.color='var(--text-3)'">×</button>
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
    const active = (connections || []).find(c => c.id === activeConnectionId);
    const label  = active ? (active.label || active.host) : 'Local';

    // Header pill shows what we're currently connected to.
    document.getElementById('connection-status').textContent = label;

    // The "Current Connection" card is the fixed local connection card —
    // its name and detail stay as configured and must not be overwritten
    // by the active remote connection. Remote cards keep their own labels
    // in the saved list below.
    document.getElementById('current-conn-display').textContent = 'Local';
    document.getElementById('current-conn-detail').textContent  = 'localhost';
    document.getElementById('local-conn-dot').classList.toggle('hidden', !!active);
    document.getElementById('local-connect-btn').classList.toggle('hidden', !active);

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
        closeSidebar();
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
        clearConnForm();
        switchView('jobs');
        await loadConnections();
        showError('✓ Connection saved');
    } catch (err) {
        showError('Failed to save connection: ' + err.message);
    }
}

function clearConnForm() {
    ['conn-host','conn-user','conn-keypath','conn-label'].forEach(id => {
        document.getElementById(id).value = '';
    });
    document.getElementById('conn-port').value = '22';
    document.getElementById('conn-test-status').textContent = '';
}

// Connection UI wiring
document.getElementById('add-conn-btn').addEventListener('click', () => switchView('add-conn'));
document.getElementById('conn-test').addEventListener('click', testConnection);
document.getElementById('conn-save').addEventListener('click', saveConnection);
document.getElementById('conn-cancel').addEventListener('click', () => {
    clearConnForm();
    switchView('jobs');
});

// ── Jobs ───────────────────────────────────────────────────────────────────
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

function formatRelativeTime(ts) {
    if (!ts) return 'Never';
    const diff = Date.now() - ts;
    const s = Math.floor(diff / 1000);
    const m = Math.floor(s / 60);
    const h = Math.floor(m / 60);
    const d = Math.floor(h / 24);
    if (s < 60) return `${s}s ago`;
    if (m < 60) return `${m}m ago`;
    if (h < 24) return `${h}h ago`;
    return `${d}d ago`;
}

function escapeHtml(str) {
    return String(str == null ? '' : str)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
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
        tbody.innerHTML = '<tr><td colspan="4" class="empty-cell">No cron jobs yet. Click <strong>Add New Job</strong> to get started.</td></tr>';
        return;
    }
    tbody.innerHTML = jobs.map((job, idx) => {
        const lastRun   = formatRelativeTime(job.LastRun);
        const hasRun    = !!job.LastRun;
        const isSuccess = job.LastStatus === 'success';
        const statusCls = hasRun ? (isSuccess ? 'ok' : 'fail') : 'none';
        const statusIcon= hasRun ? (isSuccess ? '✓' : '✗') : '—';
        const title     = job.Comment || job.Command;
        const subtitle  = job.Comment ? job.Command : '';
        const disabledBadge = job.Inactive ? '<div class="disabled-tag">⏸ Disabled</div>' : '';
        return `
        <tr class="job-row${job.Inactive ? ' inactive' : ''}">
            <td class="job-name-cell">
                <div class="job-title" title="${escapeHtml(title)}">${escapeHtml(title)}</div>
                ${disabledBadge}
                ${subtitle ? `<div class="job-sub" title="${escapeHtml(subtitle)}">${escapeHtml(subtitle)}</div>` : ''}
                <code class="job-sched">${escapeHtml(job.Schedule)}</code>
            </td>
            <td class="job-lastrun">${lastRun}</td>
            <td class="job-status"><span class="status-badge ${statusCls}">${statusIcon}</span></td>
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
    document.getElementById('form-title').textContent = 'Update Job';
    document.getElementById('submit-btn').textContent = 'Update Job';
    document.getElementById('cancel-edit-btn').classList.remove('hidden');
    switchView('add-job');
    document.getElementById('schedule-input').focus();
}

function cancelEdit() {
    editIndex = -1;
    document.getElementById('add-job-form').reset();
    document.getElementById('form-title').textContent = 'Add New Job';
    document.getElementById('submit-btn').textContent = 'Add Job';
    document.getElementById('cancel-edit-btn').classList.add('hidden');
}

function cancelAndGoBack() {
    cancelEdit();
    switchView('jobs');
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
        await loadJobs();
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
    cancelAndGoBack();
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
        cancelEdit();
        switchView('jobs');
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

// ── Home / brand navigation ────────────────────────────────────────────────
// goHome(): back to the cron jobs list for the current connection. Used by
// the header brand (hourglass logo + name) and the View Logs back button.
function goHome() {
    switchView('jobs');
    loadJobs();
    closeSidebar();
}

// Wire the header brand (hourglass logo + "Hourglass" text) to go home.
const brandHome = document.getElementById('brand-home');
if (brandHome) {
    brandHome.addEventListener('click', goHome);
    brandHome.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            goHome();
        }
    });
}

// Wire logs button
document.getElementById('nav-logs').addEventListener('click', () => switchView('logs'));

// Polling: jobs every 30s, logs every 5s when active
let _lastView = 'jobs';
setInterval(() => {
    const v = document.getElementById('view-jobs').classList.contains('hidden') ? 'other' : 'jobs';
    if (v === 'jobs') loadJobs();
    if (!document.getElementById('view-logs').classList.contains('hidden')) loadLogs();
}, 5000);

// ── Log Viewer ─────────────────────────────────────────────────────────────
async function loadLogs() {
    try {
        const resp = await fetch('/api/logs');
        if (!resp.ok) throw new Error('Failed to fetch logs');
        const data = await resp.json();

        document.getElementById('log-path').textContent = data.path || '';

        const el = document.getElementById('log-content');
        const wasAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;

        el.textContent = data.content || 'No log entries yet.';

        if (document.getElementById('log-autoscroll').checked && wasAtBottom) {
            el.scrollTop = el.scrollHeight;
        }
    } catch (err) {
        console.error('Failed to load logs:', err);
        document.getElementById('log-content').textContent = 'Failed to load logs: ' + err.message;
    }
}
