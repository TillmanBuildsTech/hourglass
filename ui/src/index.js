
let jobs = [];
let deleteIndex = -1;
let editIndex = -1;
let isLocalOnly = false;
let currentConnection = { host: 'localhost', port: 8080 };
let activeConnectionId = '';

// Connection Management
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
        document.getElementById('add-conn-btn').style.display = 'block';
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
                return `
                <div class="p-3 bg-white border ${isActive ? 'border-blue-400' : 'border-gray-200'} rounded flex justify-between items-center">
                    <div>
                        <p class="text-sm font-medium text-gray-900">${escapeHtml(conn.label || conn.host)}${isActive ? ' <span class="text-xs text-blue-600">(current)</span>' : ''}</p>
                        <p class="text-xs text-gray-600">${escapeHtml(conn.user)}@${escapeHtml(conn.host)}:${escapeHtml(conn.port)}</p>
                    </div>
                    <div class="flex gap-2">
                        ${isActive ? '' : `<button onclick="switchConnection('${escapeHtml(conn.id)}')" class="text-xs text-blue-600 hover:text-blue-900">Connect</button>`}
                        <button onclick="removeConnection('${escapeHtml(conn.id)}')" class="text-xs text-red-600 hover:text-red-900">Remove</button>
                    </div>
                </div>
            `;
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
    const label = active ? (active.label || active.host) : 'Local';
    const detail = active ? `${active.user}@${active.host}:${active.port}` : 'localhost:8080 (local)';

    document.getElementById('connection-status').textContent = label;
    document.getElementById('current-conn-display').textContent = detail;
    document.getElementById('switch-local-btn').classList.toggle('hidden', !active);
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

        document.getElementById('connections-panel').classList.add('hidden');
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
    const host = document.getElementById('conn-host').value.trim();
    const port = parseInt(document.getElementById('conn-port').value) || 22;
    const user = document.getElementById('conn-user').value.trim();
    const keyPath = document.getElementById('conn-keypath').value.trim();
    const statusEl = document.getElementById('conn-test-status');

    if (!host || !user || !keyPath) {
        statusEl.textContent = '❌ Please fill in all fields';
        statusEl.className = 'text-xs text-red-600 mt-1';
        return;
    }

    statusEl.textContent = '⏳ Testing...';
    statusEl.className = 'text-xs text-gray-600 mt-1';

    try {
        const resp = await fetch('/api/connections/test', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ host, port, user, key_path: keyPath })
        });

        if (!resp.ok) {
            const err = await resp.json();
            statusEl.textContent = '❌ ' + err.error;
            statusEl.className = 'text-xs text-red-600 mt-1';
            return;
        }

        statusEl.textContent = '✓ Connection successful';
        statusEl.className = 'text-xs text-green-600 mt-1';
    } catch (err) {
        statusEl.textContent = '❌ ' + err.message;
        statusEl.className = 'text-xs text-red-600 mt-1';
    }
}

async function saveConnection() {
    if (isLocalOnly) {
        showError('Cannot add remote connections when running locally');
        return;
    }

    const host = document.getElementById('conn-host').value.trim();
    const port = parseInt(document.getElementById('conn-port').value) || 22;
    const user = document.getElementById('conn-user').value.trim();
    const keyPath = document.getElementById('conn-keypath').value.trim();
    const label = document.getElementById('conn-label').value.trim();

    if (!host || !user || !keyPath) {
        showError('Hostname, username, and key path are required');
        return;
    }

    const id = `${host}-${user}-${Date.now()}`;

    try {
        const resp = await fetch('/api/connections', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id, host, port, user, key_path: keyPath, label
            })
        });

        if (!resp.ok) {
            const err = await resp.json();
            showError('Failed to save connection: ' + err.error);
            return;
        }

        document.getElementById('conn-host').value = '';
        document.getElementById('conn-port').value = '22';
        document.getElementById('conn-user').value = '';
        document.getElementById('conn-keypath').value = '';
        document.getElementById('conn-label').value = '';
        document.getElementById('conn-test-status').textContent = '';
        document.getElementById('add-connection-form').classList.add('hidden');
        document.getElementById('add-conn-btn').style.display = 'block';

        await loadConnections();
    } catch (err) {
        showError('Failed to save connection: ' + err.message);
    }
}

// Connection UI handlers
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
    document.getElementById('add-conn-btn').style.display = 'block';
    document.getElementById('conn-test-status').textContent = '';
});

// Jobs Management
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

    const now = Date.now();
    const diff = now - timestamp;
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (seconds < 60) return `${seconds}s ago`;
    if (minutes < 60) return `${minutes}m ago`;
    if (hours < 24) return `${hours}h ago`;
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
    run: '<svg viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4"><path d="M6.5 4.6c0-.9 1-1.5 1.8-1l7.6 4.9a1.2 1.2 0 0 1 0 2l-7.6 5c-.8.5-1.8-.1-1.8-1V4.6Z"/></svg>',
    pause: '<svg viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4"><path d="M6 4.5a1 1 0 0 1 1-1h1.5a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-11Zm5.5 0a1 1 0 0 1 1-1H14a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1h-1.5a1 1 0 0 1-1-1v-11Z"/></svg>',
    edit: '<svg viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4"><path d="M13.6 2.8a1.6 1.6 0 0 1 2.3 0l1.3 1.3a1.6 1.6 0 0 1 0 2.3L16 7.6 12.4 4l1.2-1.2ZM11 5.4 3 13.4V17h3.6l8-8L11 5.4Z"/></svg>',
    delete: '<svg viewBox="0 0 20 20" fill="currentColor" class="w-4 h-4"><path fill-rule="evenodd" d="M8 2a1 1 0 0 0-1 1v1H4a1 1 0 1 0 0 2h.4l.7 10.1A2 2 0 0 0 7.1 18h5.8a2 2 0 0 0 2-1.9L15.6 6h.4a1 1 0 1 0 0-2h-3V3a1 1 0 0 0-1-1H8Zm1 2h2V4H9v0Zm-1.6 4a.8.8 0 0 1 1.6 0v6a.8.8 0 0 1-1.6 0V8Zm4.8 0a.8.8 0 0 1 1.6 0v6a.8.8 0 0 1-1.6 0V8Z" clip-rule="evenodd"/></svg>',
};

function iconButton(icon, onclick, title, colorClass) {
    return `<button onclick="${onclick}" class="p-1.5 rounded hover:bg-gray-100 ${colorClass}" title="${title}" aria-label="${title}">${ICONS[icon]}</button>`;
}

function renderJobs() {
    const tbody = document.getElementById('jobs-table');
    if (jobs.length === 0) {
        tbody.innerHTML = '<tr class="text-center"><td colspan="6" class="px-6 py-12 text-gray-500">No cron jobs</td></tr>';
        return;
    }

    tbody.innerHTML = jobs.map((job, idx) => {
        const lastRun = formatRelativeTime(job.LastRun);
        const statusClass = !job.LastRun ? 'text-gray-400' : (job.LastStatus === 'success' ? 'text-green-600' : 'text-red-600');
        const statusText = !job.LastRun ? '-' : (job.LastStatus === 'success' ? '✓' : '✗');
        const stateText = job.Inactive ? '🚫 Disabled' : '✓ Enabled';
        const stateClass = job.Inactive ? 'text-gray-500' : 'text-green-600';

        return `
            <tr class="hover:bg-gray-50 ${job.Inactive ? 'opacity-60' : ''}">
                <td class="px-6 py-4 text-sm font-mono text-gray-900">${escapeHtml(job.Schedule)}</td>
                <td class="px-6 py-4 text-sm text-gray-900 truncate max-w-[240px]" title="${escapeHtml(job.Command)}">${escapeHtml(job.Command)}</td>
                <td class="px-6 py-4 text-sm text-gray-600">${lastRun}</td>
                <td class="px-6 py-4 text-sm font-semibold ${statusClass}">${statusText}</td>
                <td class="px-6 py-4 text-sm text-center ${stateClass}">${stateText}</td>
                <td class="px-6 py-4 text-sm text-right">
                    <div class="flex justify-end items-center gap-1">
                        ${!job.Inactive ? iconButton('run', `executeJob(${idx})`, 'Run now', 'text-orange-600 hover:text-orange-900') : ''}
                        ${iconButton(job.Inactive ? 'run' : 'pause', `toggleJob(${idx})`, job.Inactive ? 'Enable' : 'Disable', 'text-yellow-600 hover:text-yellow-900')}
                        ${iconButton('edit', `editJob(${idx})`, 'Edit', 'text-blue-600 hover:text-blue-900')}
                        ${iconButton('delete', `deleteJob(${idx})`, 'Delete', 'text-red-600 hover:text-red-900')}
                    </div>
                </td>
            </tr>
        `;
    }).join('');
}

function editJob(idx) {
    editIndex = idx;
    const job = jobs[idx];
    document.getElementById('schedule-input').value = job.Schedule;
    document.getElementById('command-input').value = job.Command;
    document.getElementById('comment-input').value = job.Comment || '';
    document.getElementById('form-title').textContent = 'Update Job';
    document.getElementById('submit-btn').textContent = 'Update Job';
    document.getElementById('cancel-edit-btn').classList.remove('hidden');
    document.getElementById('schedule-input').focus();
}

function cancelEdit() {
    editIndex = -1;
    document.getElementById('add-job-form').reset();
    document.getElementById('form-title').textContent = 'Add New Job';
    document.getElementById('submit-btn').textContent = 'Add Job';
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
    const command = document.getElementById('command-input').value;
    const comment = document.getElementById('comment-input').value;

    try {
        if (editIndex >= 0) {
            // Update existing job
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
            // Add new job
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

function showError(msg) {
    const banner = document.getElementById('error-banner');
    banner.textContent = msg;
    banner.classList.remove('hidden');
}

function clearError() {
    document.getElementById('error-banner').classList.add('hidden');
}

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

initVersion();
initConnections();
loadJobs();
setInterval(loadJobs, 30000);
