// API Base URL
const API_BASE = '/api/v1';

// WebSocket connection
let ws = null;

// Initialize app
document.addEventListener('DOMContentLoaded', () => {
    initNavigation();
    initWebSocket();
    loadDashboard();
    loadTasks();
    loadBackends();
    loadHistory();
});

// Navigation
function initNavigation() {
    document.querySelectorAll('.nav-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const view = btn.dataset.view;
            switchView(view);
        });
    });
}

function switchView(viewName) {
    // Update nav buttons
    document.querySelectorAll('.nav-btn').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.view === viewName);
    });

    // Update views
    document.querySelectorAll('.view').forEach(view => {
        view.classList.toggle('active', view.id === `${viewName}-view`);
    });

    // Reload data for the view
    switch(viewName) {
        case 'dashboard':
            loadDashboard();
            break;
        case 'tasks':
            loadTasks();
            break;
        case 'backends':
            loadBackends();
            break;
        case 'history':
            loadHistory();
            break;
    }
}

// WebSocket
function initWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}${API_BASE}/ws/progress`;

    ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        handleProgressEvent(data);
    };

    ws.onclose = () => {
        console.log('WebSocket closed, reconnecting...');
        setTimeout(initWebSocket, 3000);
    };
}

function handleProgressEvent(event) {
    console.log('Progress event:', event);

    switch(event.type) {
        case 'execution_started':
        case 'execution_completed':
        case 'execution_failed':
            loadDashboard();
            loadHistory();
            break;
        case 'archive_progress':
        case 'upload_progress':
            updateProgressBar(event.data);
            break;
    }
}

function updateProgressBar(data) {
    const progress = data.progress_percent || 0;
    console.log(`Progress: ${progress}%`);
}

// Dashboard
async function loadDashboard() {
    try {
        const [stats, executions] = await Promise.all([
            fetch(`${API_BASE}/system/stats`).then(r => r.json()),
            fetch(`${API_BASE}/executions?per_page=5`).then(r => r.json())
        ]);

        if (stats.success) {
            updateStats(stats.data);
        }

        if (executions.success) {
            updateRecentActivity(executions.data);
        }
    } catch (error) {
        console.error('Failed to load dashboard:', error);
    }
}

function updateStats(stats) {
    document.getElementById('stat-tasks').textContent = stats.tasks.enabled;
    document.getElementById('stat-backends').textContent = stats.backends.total;
    document.getElementById('stat-executions').textContent = stats.executions?.total || 0;

    const successRate = stats.executions?.total > 0
        ? Math.round((stats.executions.success / stats.executions.total) * 100)
        : 0;
    document.getElementById('stat-success-rate').textContent = `${successRate}%`;
}

function updateRecentActivity(executions) {
    const container = document.getElementById('recent-activity');

    if (!executions || executions.length === 0) {
        container.innerHTML = '<p class="empty-state">No recent activity</p>';
        return;
    }

    container.innerHTML = executions.map(exec => `
        <div class="card">
            <div class="card-header">
                <span>${exec.task_name}</span>
                <span class="badge badge-${exec.status === 'success' ? 'success' : exec.status === 'failed' ? 'error' : 'running'}">
                    ${exec.status}
                </span>
            </div>
            <div class="card-footer">
                <span>${new Date(exec.started_at).toLocaleString()}</span>
                <span>${exec.duration_ms ? Math.round(exec.duration_ms / 1000) + 's' : '-'}</span>
            </div>
        </div>
    `).join('');
}

// Tasks
async function loadTasks() {
    try {
        const response = await fetch(`${API_BASE}/tasks`);
        const data = await response.json();

        if (data.success) {
            renderTasks(data.data);
        }
    } catch (error) {
        console.error('Failed to load tasks:', error);
    }
}

function renderTasks(tasks) {
    const container = document.getElementById('tasks-list');

    if (!tasks || tasks.length === 0) {
        container.innerHTML = '<p class="empty-state">No tasks configured. Create your first task!</p>';
        return;
    }

    container.innerHTML = tasks.map(task => `
        <div class="card">
            <div class="card-header">
                <div>
                    <div class="card-title">${task.name}</div>
                    <div style="color: #666; font-size: 0.85rem;">${task.description || ''}</div>
                </div>
                <div class="card-actions">
                    <button class="btn btn-sm btn-primary" onclick="executeTask('${task.id}')">Run Now</button>
                    <button class="btn btn-sm" onclick="dryRunTask('${task.id}')">Dry Run</button>
                    <button class="btn btn-sm" onclick="editTask('${task.id}')">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteTask('${task.id}')">Delete</button>
                </div>
            </div>
            <div class="card-body">
                <p><strong>Source:</strong> ${task.source_path}</p>
                <p><strong>Schedule:</strong> ${getScheduleText(task.schedule)}</p>
                <p><strong>Backends:</strong> ${task.backend_ids.length}</p>
            </div>
            <div class="card-footer">
                <span class="badge badge-${task.enabled ? 'success' : 'disabled'}">
                    ${task.enabled ? 'Enabled' : 'Disabled'}
                </span>
                <span>${task.stats ? `${task.stats.success_count} successes` : 'No executions yet'}</span>
            </div>
        </div>
    `).join('');
}

function getScheduleText(schedule) {
    if (schedule.type === 'simple') {
        return schedule.simple_type.charAt(0).toUpperCase() + schedule.simple_type.slice(1);
    } else if (schedule.type === 'cron') {
        return `Cron: ${schedule.cron_expr}`;
    } else {
        return 'Manual';
    }
}

async function executeTask(taskId) {
    try {
        const response = await fetch(`${API_BASE}/tasks/${taskId}/execute`, {
            method: 'POST'
        });
        const data = await response.json();

        if (data.success) {
            showToast('Task execution started!', 'success');
            loadDashboard();
        } else {
            showToast('Failed to execute task: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function dryRunTask(taskId) {
    try {
        // Show loading message
        const modalBody = document.getElementById('modal-body');
        modalBody.innerHTML = '<div style="text-align: center; padding: 2rem;"><div style="font-size: 2rem;">⏳</div><p>Analyzing files and backends...</p></div>';
        showModal();

        const response = await fetch(`${API_BASE}/tasks/${taskId}/execute?dry_run=true`, {
            method: 'POST'
        });
        const data = await response.json();

        if (data.success) {
            showDryRunResults(data.data);
        } else {
            closeModal();
            showToast('Failed to run dry run: ' + data.error.message, 'error');
        }
    } catch (error) {
        closeModal();
        showToast('Error: ' + error.message, 'error');
    }
}

function showDryRunResults(result) {
    const formatBytes = (bytes) => {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    };

    const formatNumber = (num) => {
        return num.toLocaleString();
    };

    // Build summary section
    let summaryHTML = `
        <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
            <h3 style="margin: 0 0 1rem 0; color: #00ff88;">📊 Summary</h3>
            <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem;">
                <div>
                    <div style="color: #666; font-size: 0.85rem;">Mode</div>
                    <div style="font-size: 1.2rem; font-weight: bold;">${result.mode === 'archive' ? '📦 Archive' : '🔄 Sync'}</div>
                </div>
                <div>
                    <div style="color: #666; font-size: 0.85rem;">Total Files</div>
                    <div style="font-size: 1.2rem; font-weight: bold;">${formatNumber(result.files_summary.total_files)}</div>
                </div>
                <div>
                    <div style="color: #666; font-size: 0.85rem;">Total Size</div>
                    <div style="font-size: 1.2rem; font-weight: bold;">${formatBytes(result.files_summary.total_size)}</div>
                </div>
                <div>
                    <div style="color: #666; font-size: 0.85rem;">Directories</div>
                    <div style="font-size: 1.2rem; font-weight: bold;">${formatNumber(result.files_summary.total_dirs)}</div>
                </div>
            </div>
        </div>
    `;

    // Archive details (if archive mode)
    if (result.archive_details) {
        summaryHTML += `
            <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
                <h3 style="margin: 0 0 1rem 0; color: #00ff88;">📦 Archive Details</h3>
                <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 1rem;">
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">Archive Name</div>
                        <div style="font-size: 0.95rem; word-break: break-all;">${result.archive_details.archive_name}</div>
                    </div>
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">Estimated Size</div>
                        <div style="font-size: 1.2rem; font-weight: bold;">${formatBytes(result.archive_details.estimated_archive_size)}</div>
                    </div>
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">Compression</div>
                        <div style="font-size: 1.2rem; font-weight: bold;">${Math.round((1 - result.archive_details.compression_ratio) * 100)}% reduction</div>
                    </div>
                </div>
            </div>
        `;
    }

    // Sync details (if sync mode)
    if (result.sync_details) {
        summaryHTML += `
            <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
                <h3 style="margin: 0 0 1rem 0; color: #00ff88;">🔄 Sync Analysis</h3>
                <div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 1rem;">
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">To Upload</div>
                        <div style="font-size: 1.2rem; font-weight: bold; color: #00ff88;">${result.sync_details.upload_count} files</div>
                        <div style="font-size: 0.85rem; color: #888;">${formatBytes(result.sync_details.bytes_to_upload)}</div>
                    </div>
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">Unchanged</div>
                        <div style="font-size: 1.2rem; font-weight: bold;">${result.sync_details.skip_count} files</div>
                    </div>
                    <div>
                        <div style="color: #666; font-size: 0.85rem;">To Delete</div>
                        <div style="font-size: 1.2rem; font-weight: bold; color: #ff6b6b;">${result.sync_details.delete_count} files</div>
                    </div>
                </div>
            </div>
        `;
    }

    // Backend status
    let backendsHTML = `
        <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
            <h3 style="margin: 0 0 1rem 0; color: #00ff88;">🔌 Backend Status</h3>
            <div style="display: grid; gap: 0.75rem;">
    `;

    result.backend_plans.forEach(backend => {
        const statusIcon = backend.available ? '✅' : '❌';
        const statusColor = backend.available ? '#00ff88' : '#ff6b6b';
        backendsHTML += `
            <div style="background: #1a1a1a; padding: 1rem; border-radius: 6px; border-left: 3px solid ${statusColor};">
                <div style="display: flex; justify-content: space-between; align-items: start;">
                    <div style="flex: 1;">
                        <div style="font-weight: bold; margin-bottom: 0.25rem;">${statusIcon} ${backend.backend_name}</div>
                        <div style="color: #888; font-size: 0.85rem;">${backend.backend_type}</div>
                        ${backend.remote_path ? `<div style="color: #666; font-size: 0.85rem; margin-top: 0.25rem;">Path: ${backend.remote_path}</div>` : ''}
                    </div>
                    ${backend.error_message ? `<div style="color: #ff6b6b; font-size: 0.85rem; text-align: right; max-width: 50%;">${backend.error_message}</div>` : ''}
                </div>
            </div>
        `;
    });

    backendsHTML += `
            </div>
        </div>
    `;

    // Top files section
    if (result.files_summary.top_files && result.files_summary.top_files.length > 0) {
        let topFilesHTML = `
            <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px; margin-bottom: 1.5rem;">
                <h3 style="margin: 0 0 1rem 0; color: #00ff88;">📁 Largest Files (Top 10)</h3>
                <div style="max-height: 300px; overflow-y: auto;">
                    <table style="width: 100%; border-collapse: collapse;">
                        <thead>
                            <tr style="border-bottom: 1px solid #444;">
                                <th style="text-align: left; padding: 0.5rem; color: #888; font-size: 0.85rem;">File</th>
                                <th style="text-align: right; padding: 0.5rem; color: #888; font-size: 0.85rem;">Size</th>
                            </tr>
                        </thead>
                        <tbody>
        `;

        result.files_summary.top_files.forEach(file => {
            topFilesHTML += `
                <tr style="border-bottom: 1px solid #333;">
                    <td style="padding: 0.5rem; font-size: 0.9rem; word-break: break-all;">${file.relative_path}</td>
                    <td style="padding: 0.5rem; text-align: right; white-space: nowrap;">${formatBytes(file.size)}</td>
                </tr>
            `;
        });

        topFilesHTML += `
                        </tbody>
                    </table>
                </div>
            </div>
        `;
        summaryHTML += topFilesHTML;
    }

    // File types breakdown
    if (result.files_summary.file_types && Object.keys(result.files_summary.file_types).length > 0) {
        const sortedTypes = Object.entries(result.files_summary.file_types)
            .sort((a, b) => b[1] - a[1])
            .slice(0, 10);

        let fileTypesHTML = `
            <div style="background: #2a2a2a; padding: 1.5rem; border-radius: 8px;">
                <h3 style="margin: 0 0 1rem 0; color: #00ff88;">📊 File Types</h3>
                <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 0.75rem;">
        `;

        sortedTypes.forEach(([ext, count]) => {
            fileTypesHTML += `
                <div style="background: #1a1a1a; padding: 0.75rem; border-radius: 6px; text-align: center;">
                    <div style="font-weight: bold; font-size: 1.1rem;">${formatNumber(count)}</div>
                    <div style="color: #888; font-size: 0.85rem;">${ext}</div>
                </div>
            `;
        });

        fileTypesHTML += `
                </div>
            </div>
        `;
        summaryHTML += fileTypesHTML;
    }

    // Set modal content and show
    const modalBody = document.getElementById('modal-body');
    modalBody.innerHTML = `
        <h2 style="margin: 0 0 1.5rem 0;">Dry Run Results - ${result.task_name}</h2>
        ${summaryHTML}
    `;
}

async function editTask(taskId) {
    try {
        // Fetch task and backends
        const [taskResponse, backendsResponse] = await Promise.all([
            fetch(`${API_BASE}/tasks/${taskId}`),
            fetch(`${API_BASE}/backends`)
        ]);

        const taskData = await taskResponse.json();
        const backendsData = await backendsResponse.json();

        if (!taskData.success) {
            showToast('Failed to load task', 'error');
            return;
        }

        const task = taskData.data;
        const backends = backendsData.success ? backendsData.data : [];

        const modalBody = document.getElementById('modal-body');
        modalBody.innerHTML = `
            <h2>Edit Task</h2>
            <form id="edit-task-form">
                <div class="form-group">
                    <label>Task Name *</label>
                    <input type="text" name="name" value="${task.name}" required>
                </div>
                <div class="form-group">
                    <label>Description</label>
                    <textarea name="description" rows="3">${task.description || ''}</textarea>
                </div>
                <div class="form-group">
                    <label>Source Path *</label>
                    <input type="text" name="source_path" value="${task.source_path}" required>
                </div>
                <div class="form-group">
                    <label>Storage Backend(s) *</label>
                    <div class="backend-selector">
                        ${backends.map(backend => `
                            <label class="backend-option">
                                <input type="checkbox" name="backend" value="${backend.id}"
                                    ${task.backend_ids.includes(backend.id) ? 'checked' : ''}>
                                <span class="backend-option-content">
                                    <span class="backend-option-name">${backend.name}</span>
                                    <span class="backend-option-type">${backend.type.toUpperCase()}</span>
                                </span>
                            </label>
                        `).join('')}
                    </div>
                </div>
                <div class="form-group">
                    <label>Schedule Type *</label>
                    <select name="schedule_type" onchange="updateScheduleFields(this.value)">
                        <option value="simple" ${task.schedule.type === 'simple' ? 'selected' : ''}>Simple</option>
                        <option value="cron" ${task.schedule.type === 'cron' ? 'selected' : ''}>Cron</option>
                        <option value="manual" ${task.schedule.type === 'manual' ? 'selected' : ''}>Manual</option>
                    </select>
                </div>
                <div class="form-group" id="simple-schedule" style="display: ${task.schedule.type === 'simple' ? 'block' : 'none'};">
                    <label>Frequency</label>
                    <select name="simple_type">
                        <option value="hourly" ${task.schedule.simple_type === 'hourly' ? 'selected' : ''}>Hourly</option>
                        <option value="daily" ${task.schedule.simple_type === 'daily' ? 'selected' : ''}>Daily</option>
                        <option value="weekly" ${task.schedule.simple_type === 'weekly' ? 'selected' : ''}>Weekly</option>
                        <option value="monthly" ${task.schedule.simple_type === 'monthly' ? 'selected' : ''}>Monthly</option>
                    </select>
                </div>
                <div class="form-group" id="cron-schedule" style="display: ${task.schedule.type === 'cron' ? 'block' : 'none'};">
                    <label>Cron Expression</label>
                    <input type="text" name="cron_expr" value="${task.schedule.cron_expr || ''}" placeholder="0 2 * * *">
                </div>
                <div class="form-group">
                    <label>Use Timestamp in Filename</label>
                    <select name="use_timestamp" onchange="updateRetentionVisibility(this.value)">
                        <option value="true" ${task.archive_options.use_timestamp ? 'selected' : ''}>Yes (Point-in-time backups)</option>
                        <option value="false" ${!task.archive_options.use_timestamp ? 'selected' : ''}>No (Mirror/overwrite)</option>
                    </select>
                </div>
                <div class="form-group" id="retention-field" style="display: ${task.archive_options.use_timestamp ? 'block' : 'none'};">
                    <label>Retention (Keep Last N Backups, 0 = unlimited)</label>
                    <input type="number" name="keep_last" value="${task.retention_policy.keep_last}" min="0">
                    <small style="color: #666;">Only applies to point-in-time backups</small>
                </div>
                <div class="form-group">
                    <label>Status</label>
                    <select name="enabled">
                        <option value="true" ${task.enabled ? 'selected' : ''}>Enabled</option>
                        <option value="false" ${!task.enabled ? 'selected' : ''}>Disabled</option>
                    </select>
                </div>
                <div class="form-actions">
                    <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                    <button type="submit" class="btn btn-primary">Save Changes</button>
                </div>
            </form>
        `;

        document.getElementById('edit-task-form').addEventListener('submit', (e) => handleEditTask(e, taskId));
        showModal();
    } catch (error) {
        showToast('Error loading task: ' + error.message, 'error');
    }
}

async function handleEditTask(e, taskId) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    const selectedBackends = Array.from(form.querySelectorAll('input[name="backend"]:checked'))
        .map(cb => cb.value);

    if (selectedBackends.length === 0) {
        showToast('Please select at least one backend', 'warning');
        return;
    }

    const task = {
        name: formData.get('name'),
        description: formData.get('description'),
        source_path: formData.get('source_path'),
        backend_ids: selectedBackends,
        schedule: {
            type: formData.get('schedule_type'),
            simple_type: formData.get('simple_type'),
            cron_expr: formData.get('cron_expr')
        },
        archive_options: {
            format: 'tar.gz',
            compression: 'gzip',
            use_timestamp: formData.get('use_timestamp') === 'true'
        },
        retention_policy: {
            keep_last: parseInt(formData.get('keep_last'))
        },
        enabled: formData.get('enabled') === 'true'
    };

    try {
        const response = await fetch(`${API_BASE}/tasks/${taskId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(task)
        });
        const data = await response.json();

        if (data.success) {
            closeModal();
            loadTasks();
        } else {
            showToast('Failed to update task: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function deleteTask(taskId) {
    if (!confirm('Are you sure you want to delete this task?')) {
        return;
    }

    try {
        const response = await fetch(`${API_BASE}/tasks/${taskId}`, {
            method: 'DELETE'
        });
        const data = await response.json();

        if (data.success) {
            loadTasks();
            showToast('Task deleted successfully', 'success');
        } else {
            showToast('Failed to delete task: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function showCreateTask() {
    // Load backends first
    const response = await fetch(`${API_BASE}/backends`);
    const data = await response.json();
    const backends = data.success ? data.data : [];

    const modalBody = document.getElementById('modal-body');
    modalBody.innerHTML = `
        <h2>Create New Task</h2>
        <form id="create-task-form">
            <div class="form-group">
                <label>Task Name *</label>
                <input type="text" name="name" required>
            </div>
            <div class="form-group">
                <label>Description</label>
                <textarea name="description" rows="3"></textarea>
            </div>
            <div class="form-group">
                <label>Source Path *</label>
                <input type="text" name="source_path" placeholder="/data/sources/..." required>
            </div>
            <div class="form-group">
                <label>Storage Backend(s) *</label>
                ${backends.length > 0 ? `
                    <div class="backend-selector">
                        ${backends.map(backend => `
                            <label class="backend-option">
                                <input type="checkbox" name="backend" value="${backend.id}">
                                <span class="backend-option-content">
                                    <span class="backend-option-name">${backend.name}</span>
                                    <span class="backend-option-type">${backend.type.toUpperCase()}</span>
                                </span>
                            </label>
                        `).join('')}
                    </div>
                ` : `
                    <p style="color: #ff4444;">No backends configured. Please create a backend first.</p>
                `}
            </div>
            <div class="form-group">
                <label>Schedule Type *</label>
                <select name="schedule_type" onchange="updateScheduleFields(this.value)">
                    <option value="simple">Simple</option>
                    <option value="cron">Cron</option>
                    <option value="manual">Manual</option>
                </select>
            </div>
            <div class="form-group" id="simple-schedule">
                <label>Frequency</label>
                <select name="simple_type">
                    <option value="hourly">Hourly</option>
                    <option value="daily">Daily</option>
                    <option value="weekly">Weekly</option>
                    <option value="monthly">Monthly</option>
                </select>
            </div>
            <div class="form-group" id="cron-schedule" style="display: none;">
                <label>Cron Expression</label>
                <input type="text" name="cron_expr" placeholder="0 2 * * *">
            </div>
            <div class="form-group">
                <label>Backup Mode *</label>
                <select name="backup_mode" onchange="updateBackupModeFields(this.value)">
                    <option value="archive">Archive (Compressed, point-in-time or mirror)</option>
                    <option value="sync">Sync (File-by-file, for large directories)</option>
                </select>
                <small style="color: #666;">Archive: Creates compressed archive. Sync: Uploads files individually (efficient for large directories)</small>
            </div>
            <div id="archive-options">
                <div class="form-group">
                    <label>Use Timestamp in Filename</label>
                    <select name="use_timestamp" onchange="updateRetentionVisibility(this.value)">
                        <option value="true">Yes (Point-in-time backups)</option>
                        <option value="false">No (Mirror/overwrite)</option>
                    </select>
                </div>
                <div class="form-group" id="retention-field">
                    <label>Retention (Keep Last N Backups, 0 = unlimited)</label>
                    <input type="number" name="keep_last" value="7" min="0">
                    <small style="color: #666;">Only applies to point-in-time backups</small>
                </div>
            </div>
            <div id="sync-options" style="display: none;">
                <div class="form-group">
                    <label>File Comparison Method</label>
                    <select name="compare_method">
                        <option value="mtime">Modification Time (Faster)</option>
                        <option value="hash">Hash/Checksum (Slower, more accurate)</option>
                    </select>
                    <small style="color: #666;">Mtime: Quick, compares file timestamps. Hash: Slower, detects content changes</small>
                </div>
                <div class="form-group">
                    <label>Delete Remote Files</label>
                    <select name="delete_remote">
                        <option value="false">No (Safer - only add/update files)</option>
                        <option value="true">Yes (True mirror - delete remote files not in source)</option>
                    </select>
                    <small style="color: #ff4444;">Warning: True mirror will delete remote files that don't exist locally</small>
                </div>
            </div>
            <div class="form-actions">
                <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                <button type="submit" class="btn btn-primary" ${backends.length === 0 ? 'disabled' : ''}>Create Task</button>
            </div>
        </form>
    `;

    document.getElementById('create-task-form').addEventListener('submit', handleCreateTask);
    showModal();
}

function updateScheduleFields(type) {
    document.getElementById('simple-schedule').style.display = type === 'simple' ? 'block' : 'none';
    document.getElementById('cron-schedule').style.display = type === 'cron' ? 'block' : 'none';
}

function updateRetentionVisibility(useTimestamp) {
    const retentionField = document.getElementById('retention-field');
    if (retentionField) {
        retentionField.style.display = useTimestamp === 'true' ? 'block' : 'none';
    }
}

function updateBackupModeFields(mode) {
    const archiveOptions = document.getElementById('archive-options');
    const syncOptions = document.getElementById('sync-options');

    if (mode === 'sync') {
        archiveOptions.style.display = 'none';
        syncOptions.style.display = 'block';
    } else {
        archiveOptions.style.display = 'block';
        syncOptions.style.display = 'none';
    }
}

async function handleCreateTask(e) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    // Collect selected backends
    const selectedBackends = Array.from(form.querySelectorAll('input[name="backend"]:checked'))
        .map(cb => cb.value);

    if (selectedBackends.length === 0) {
        showToast('Please select at least one backend', 'warning');
        return;
    }

    const backupMode = formData.get('backup_mode');
    const task = {
        name: formData.get('name'),
        description: formData.get('description'),
        source_path: formData.get('source_path'),
        backend_ids: selectedBackends,
        schedule: {
            type: formData.get('schedule_type'),
            simple_type: formData.get('simple_type'),
            cron_expr: formData.get('cron_expr')
        },
        archive_options: backupMode === 'sync' ? {
            format: 'sync',
            compression: 'none',
            use_timestamp: false,
            sync_options: {
                compare_method: formData.get('compare_method') || 'mtime',
                delete_remote: formData.get('delete_remote') === 'true'
            }
        } : {
            format: 'tar.gz',
            compression: 'gzip',
            use_timestamp: formData.get('use_timestamp') === 'true',
            sync_options: {
                compare_method: 'mtime',
                delete_remote: false
            }
        },
        retention_policy: {
            keep_last: backupMode === 'sync' ? 0 : parseInt(formData.get('keep_last'))
        },
        enabled: true
    };

    try {
        const response = await fetch(`${API_BASE}/tasks`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(task)
        });
        const data = await response.json();

        if (data.success) {
            closeModal();
            loadTasks();
            showToast('Task created successfully', 'success');
        } else {
            showToast('Failed to create task: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

// Backends
async function loadBackends() {
    try {
        const response = await fetch(`${API_BASE}/backends`);
        const data = await response.json();

        if (data.success) {
            renderBackends(data.data);
        }
    } catch (error) {
        console.error('Failed to load backends:', error);
    }
}

function renderBackends(backends) {
    const container = document.getElementById('backends-list');

    if (!backends || backends.length === 0) {
        container.innerHTML = '<p class="empty-state">No backends configured. Add your first storage backend!</p>';
        return;
    }

    container.innerHTML = backends.map(backend => `
        <div class="card">
            <div class="card-header">
                <div>
                    <div class="card-title">${backend.name}</div>
                    <div style="color: #666; font-size: 0.85rem;">${backend.type.toUpperCase()}</div>
                </div>
                <div class="card-actions">
                    <button class="btn btn-sm" onclick="testBackend('${backend.id}')">Test</button>
                    <button class="btn btn-sm" onclick="editBackend('${backend.id}')">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteBackend('${backend.id}')">Delete</button>
                </div>
            </div>
            <div class="card-footer">
                <span class="badge badge-${backend.enabled ? 'success' : 'disabled'}">
                    ${backend.enabled ? 'Enabled' : 'Disabled'}
                </span>
                <span>${backend.last_test_status ? `Last test: ${backend.last_test_status}` : 'Never tested'}</span>
            </div>
        </div>
    `).join('');
}

async function testBackend(backendId) {
    try {
        const response = await fetch(`${API_BASE}/backends/${backendId}/test`, {
            method: 'POST'
        });
        const data = await response.json();

        if (data.success) {
            showToast('Backend test successful!', 'success');
            loadBackends();
        } else {
            showToast('Backend test failed: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function editBackend(backendId) {
    try {
        const response = await fetch(`${API_BASE}/backends/${backendId}`);
        const data = await response.json();

        if (!data.success) {
            showToast('Failed to load backend', 'error');
            return;
        }

        const backend = data.data;

        const modalBody = document.getElementById('modal-body');
        modalBody.innerHTML = `
            <h2>Edit Backend</h2>
            <form id="edit-backend-form">
                <div class="form-group">
                    <label>Backend Name *</label>
                    <input type="text" name="name" value="${backend.name}" required>
                </div>
                <div class="form-group">
                    <label>Backend Type *</label>
                    <select name="type" disabled style="background: #1a1a1a;">
                        <option value="${backend.type}" selected>${backend.type.toUpperCase()}</option>
                    </select>
                    <small style="color: #666;">Type cannot be changed</small>
                </div>
                <div id="backend-config-fields">
                    ${getBackendConfigFields(backend.type, backend.config)}
                </div>
                <div class="form-group">
                    <label>Status</label>
                    <select name="enabled">
                        <option value="true" ${backend.enabled ? 'selected' : ''}>Enabled</option>
                        <option value="false" ${!backend.enabled ? 'selected' : ''}>Disabled</option>
                    </select>
                </div>
                <div class="form-actions">
                    <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                    <button type="submit" class="btn btn-primary">Save Changes</button>
                </div>
            </form>
        `;

        document.getElementById('edit-backend-form').addEventListener('submit', (e) => handleEditBackend(e, backendId, backend.type));
        showModal();
    } catch (error) {
        showToast('Error loading backend: ' + error.message, 'error');
    }
}

async function handleEditBackend(e, backendId, backendType) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    // Collect all config fields
    const config = {};
    for (let [key, value] of formData.entries()) {
        if (key.startsWith('config_')) {
            const configKey = key.replace('config_', '');
            // Only add non-empty values
            if (value && value.trim() !== '') {
                config[configKey] = value;
            }
        }
    }

    const backend = {
        name: formData.get('name'),
        type: backendType,
        config: config,
        enabled: formData.get('enabled') === 'true'
    };

    try {
        const response = await fetch(`${API_BASE}/backends/${backendId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(backend)
        });
        const data = await response.json();

        if (data.success) {
            closeModal();
            loadBackends();
            showToast('Backend updated successfully', 'success');
        } else {
            showToast('Failed to update backend: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function deleteBackend(backendId) {
    if (!confirm('Are you sure you want to delete this backend?')) {
        return;
    }

    try {
        const response = await fetch(`${API_BASE}/backends/${backendId}`, {
            method: 'DELETE'
        });
        const data = await response.json();

        if (data.success) {
            loadBackends();
            showToast('Backend deleted successfully', 'success');
        } else {
            showToast('Failed to delete backend: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

function showCreateBackend() {
    const modalBody = document.getElementById('modal-body');
    modalBody.innerHTML = `
        <h2>Create New Backend</h2>
        <form id="create-backend-form">
            <div class="form-group">
                <label>Backend Name *</label>
                <input type="text" name="name" required>
            </div>
            <div class="form-group">
                <label>Backend Type *</label>
                <select name="type" onchange="updateBackendFields(this.value, 'create')">
                    <option value="local">Local Filesystem</option>
                    <option value="s3">AWS S3 / S3-Compatible</option>
                    <option value="gcs">Google Cloud Storage</option>
                    <option value="gdrive">Google Drive</option>
                    <option value="azure">Azure Blob Storage</option>
                    <option value="b2">Backblaze B2</option>
                </select>
            </div>
            <div id="backend-config-fields">
                ${getBackendConfigFields('local', {})}
            </div>
            <div class="form-actions">
                <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                <button type="submit" class="btn btn-primary">Create Backend</button>
            </div>
        </form>
    `;

    document.getElementById('create-backend-form').addEventListener('submit', handleCreateBackend);
    showModal();
}

function updateBackendFields(backendType, mode) {
    const container = document.getElementById('backend-config-fields');
    const existingConfig = mode === 'edit' ? getCurrentBackendConfig() : {};
    container.innerHTML = getBackendConfigFields(backendType, existingConfig);
}

function getCurrentBackendConfig() {
    // Helper to extract current config values from form (for edit mode)
    const form = document.getElementById('edit-backend-form') || document.getElementById('create-backend-form');
    const config = {};
    const inputs = form.querySelectorAll('[name^="config_"]');
    inputs.forEach(input => {
        const key = input.name.replace('config_', '');
        config[key] = input.value;
    });
    return config;
}

function getBackendConfigFields(backendType, config) {
    switch(backendType) {
        case 'local':
            return `
                <div class="form-group">
                    <label>Storage Path *</label>
                    <input type="text" name="config_path" value="${config.path || ''}" placeholder="backups/local" required>
                    <small style="color: #666;">Relative to root directory or absolute path</small>
                </div>
            `;

        case 's3':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Region *</label>
                    <input type="text" name="config_region" value="${config.region || 'us-east-1'}" placeholder="us-east-1" required>
                </div>
                <div class="form-group">
                    <label>Access Key ID</label>
                    <input type="text" name="config_access_key_id" value="${config.access_key_id || ''}" placeholder="Leave empty to use IAM role">
                    <small style="color: #666;">Optional: Uses IAM role if not provided</small>
                </div>
                <div class="form-group">
                    <label>Secret Access Key</label>
                    <input type="password" name="config_secret_access_key" value="${config.secret_access_key || ''}" placeholder="Leave empty to use IAM role">
                    <small style="color: #666;">Optional: Uses IAM role if not provided</small>
                </div>
                <div class="form-group">
                    <label>Custom Endpoint (for S3-compatible storage)</label>
                    <input type="text" name="config_endpoint" value="${config.endpoint || ''}" placeholder="https://minio.example.com:9000">
                    <small style="color: #666;">Optional: For MinIO, DigitalOcean Spaces, etc.</small>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
                <div class="form-group">
                    <label>Storage Tier (Class)</label>
                    <select name="config_storage_tier">
                        <option value="STANDARD" ${(config.storage_tier || 'STANDARD') === 'STANDARD' ? 'selected' : ''}>Standard (default, frequent access)</option>
                        <option value="STANDARD_IA" ${config.storage_tier === 'STANDARD_IA' ? 'selected' : ''}>Standard-IA (infrequent access, lower cost)</option>
                        <option value="ONEZONE_IA" ${config.storage_tier === 'ONEZONE_IA' ? 'selected' : ''}>One Zone-IA (single AZ, lowest IA cost)</option>
                        <option value="INTELLIGENT_TIERING" ${config.storage_tier === 'INTELLIGENT_TIERING' ? 'selected' : ''}>Intelligent-Tiering (automatic optimization)</option>
                        <option value="GLACIER_IR" ${config.storage_tier === 'GLACIER_IR' ? 'selected' : ''}>Glacier Instant Retrieval (archival, instant)</option>
                        <option value="GLACIER" ${config.storage_tier === 'GLACIER' ? 'selected' : ''}>Glacier Flexible Retrieval (3-5hr retrieval)</option>
                        <option value="DEEP_ARCHIVE" ${config.storage_tier === 'DEEP_ARCHIVE' ? 'selected' : ''}>Glacier Deep Archive (12hr retrieval, cheapest)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval fees/delays.</small>
                </div>
            `;

        case 'gcs':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Credentials File Path</label>
                    <input type="text" name="config_credentials_file" value="${config.credentials_file || ''}" placeholder="config/gcs-service-account.json">
                    <small style="color: #666;">Path to service account JSON key (relative to root)</small>
                </div>
                <div class="form-group">
                    <label>OR Credentials JSON</label>
                    <textarea name="config_credentials_json" rows="4" placeholder='{"type":"service_account",...}'>${config.credentials_json || ''}</textarea>
                    <small style="color: #666;">Paste service account JSON directly</small>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
                <div class="form-group">
                    <label>Storage Class</label>
                    <select name="config_storage_tier">
                        <option value="STANDARD" ${(config.storage_tier || 'STANDARD') === 'STANDARD' ? 'selected' : ''}>Standard (default, frequent access)</option>
                        <option value="NEARLINE" ${config.storage_tier === 'NEARLINE' ? 'selected' : ''}>Nearline (30-day minimum, lower cost)</option>
                        <option value="COLDLINE" ${config.storage_tier === 'COLDLINE' ? 'selected' : ''}>Coldline (90-day minimum, very low cost)</option>
                        <option value="ARCHIVE" ${config.storage_tier === 'ARCHIVE' ? 'selected' : ''}>Archive (365-day minimum, cheapest)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval fees.</small>
                </div>
            `;

        case 'azure':
            return `
                <div class="form-group">
                    <label>Container Name *</label>
                    <input type="text" name="config_container" value="${config.container || ''}" placeholder="backups" required>
                </div>
                <div class="form-group">
                    <label>Account Name *</label>
                    <input type="text" name="config_account_name" value="${config.account_name || ''}" placeholder="mystorageaccount" required>
                </div>
                <div class="form-group">
                    <label>Authentication Method *</label>
                    <select name="auth_method" onchange="updateAzureAuthFields(this.value)">
                        <option value="account_key" ${config.account_key ? 'selected' : ''}>Account Key</option>
                        <option value="sas_token" ${config.sas_token ? 'selected' : ''}>SAS Token</option>
                        <option value="connection_string" ${config.connection_string ? 'selected' : ''}>Connection String</option>
                    </select>
                </div>
                <div id="azure-auth-fields">
                    <div class="form-group" id="azure-account-key">
                        <label>Account Key</label>
                        <input type="password" name="config_account_key" value="${config.account_key || ''}" placeholder="Account key">
                    </div>
                    <div class="form-group" id="azure-sas-token" style="display: none;">
                        <label>SAS Token</label>
                        <input type="text" name="config_sas_token" value="${config.sas_token || ''}" placeholder="sv=2020-08-04&ss=b...">
                    </div>
                    <div class="form-group" id="azure-connection-string" style="display: none;">
                        <label>Connection String</label>
                        <textarea name="config_connection_string" rows="3" placeholder="DefaultEndpointsProtocol=https;AccountName=...">${config.connection_string || ''}</textarea>
                    </div>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within container</small>
                </div>
                <div class="form-group">
                    <label>Access Tier</label>
                    <select name="config_storage_tier">
                        <option value="" ${!config.storage_tier ? 'selected' : ''}>Account Default</option>
                        <option value="Hot" ${config.storage_tier === 'Hot' ? 'selected' : ''}>Hot (frequent access, highest cost)</option>
                        <option value="Cool" ${config.storage_tier === 'Cool' ? 'selected' : ''}>Cool (30-day minimum, lower cost)</option>
                        <option value="Cold" ${config.storage_tier === 'Cold' ? 'selected' : ''}>Cold (90-day minimum, very low cost)</option>
                        <option value="Archive" ${config.storage_tier === 'Archive' ? 'selected' : ''}>Archive (180-day minimum, cheapest, rehydration required)</option>
                    </select>
                    <small style="color: #666;">Choose based on access frequency. Lower tiers = lower storage cost but retrieval delays.</small>
                </div>
            `;

        case 'b2':
            return `
                <div class="form-group">
                    <label>Bucket Name *</label>
                    <input type="text" name="config_bucket" value="${config.bucket || ''}" placeholder="my-backup-bucket" required>
                </div>
                <div class="form-group">
                    <label>Application Key ID *</label>
                    <input type="text" name="config_key_id" value="${config.key_id || ''}" placeholder="Key ID" required>
                </div>
                <div class="form-group">
                    <label>Application Key *</label>
                    <input type="password" name="config_application_key" value="${config.application_key || ''}" placeholder="Application key" required>
                </div>
                <div class="form-group">
                    <label>Path Prefix</label>
                    <input type="text" name="config_prefix" value="${config.prefix || ''}" placeholder="archivist">
                    <small style="color: #666;">Optional: Organize backups within bucket</small>
                </div>
            `;

        case 'gdrive':
            return `
                <div class="form-group">
                    <label>Credentials File Path</label>
                    <input type="text" name="config_credentials_file" value="${config.credentials_file || ''}" placeholder="config/gdrive-service-account.json">
                    <small style="color: #666;">Path to service account JSON key (relative to root)</small>
                </div>
                <div class="form-group">
                    <label>OR Credentials JSON</label>
                    <textarea name="config_credentials_json" rows="4" placeholder='{"type":"service_account",...}'>${config.credentials_json || ''}</textarea>
                    <small style="color: #666;">Paste service account JSON directly</small>
                </div>
                <div class="form-group">
                    <label>Folder Name</label>
                    <input type="text" name="config_folder_name" value="${config.folder_name || 'archivist-backups'}" placeholder="archivist-backups">
                    <small style="color: #666;">Name of folder to create/use in Drive</small>
                </div>
                <div class="form-group">
                    <label>OR Specific Folder ID</label>
                    <input type="text" name="config_folder_id" value="${config.folder_id || ''}" placeholder="1aBcDeFgHiJkLmNoPqRsTuVwXyZ">
                    <small style="color: #666;">Use specific folder ID (overrides folder name)</small>
                </div>
            `;

        default:
            return '<p>Unknown backend type</p>';
    }
}

function updateAzureAuthFields(method) {
    document.getElementById('azure-account-key').style.display = method === 'account_key' ? 'block' : 'none';
    document.getElementById('azure-sas-token').style.display = method === 'sas_token' ? 'block' : 'none';
    document.getElementById('azure-connection-string').style.display = method === 'connection_string' ? 'block' : 'none';
}

async function handleCreateBackend(e) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

    // Collect all config fields
    const config = {};
    for (let [key, value] of formData.entries()) {
        if (key.startsWith('config_')) {
            const configKey = key.replace('config_', '');
            // Only add non-empty values
            if (value && value.trim() !== '') {
                config[configKey] = value;
            }
        }
    }

    const backend = {
        name: formData.get('name'),
        type: formData.get('type'),
        config: config,
        enabled: true
    };

    try {
        const response = await fetch(`${API_BASE}/backends`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(backend)
        });
        const data = await response.json();

        if (data.success) {
            closeModal();
            loadBackends();
            showToast('Backend created successfully', 'success');
        } else {
            showToast('Failed to create backend: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

// History
async function loadHistory() {
    try {
        const response = await fetch(`${API_BASE}/executions?per_page=20`);
        const data = await response.json();

        if (data.success) {
            renderHistory(data.data);
        }
    } catch (error) {
        console.error('Failed to load history:', error);
    }
}

function renderHistory(executions) {
    const container = document.getElementById('history-list');

    if (!executions || executions.length === 0) {
        container.innerHTML = '<p class="empty-state">No execution history</p>';
        return;
    }

    container.innerHTML = executions.map(exec => `
        <div class="card">
            <div class="card-header">
                <div>
                    <div class="card-title">${exec.task_name}</div>
                    <div style="color: #666; font-size: 0.85rem;">${new Date(exec.started_at).toLocaleString()}</div>
                </div>
                <span class="badge badge-${exec.status === 'success' ? 'success' : exec.status === 'failed' ? 'error' : 'running'}">
                    ${exec.status}
                </span>
            </div>
            <div class="card-body">
                ${exec.archive_size ? `<p><strong>Size:</strong> ${formatBytes(exec.archive_size)}</p>` : ''}
                ${exec.duration_ms ? `<p><strong>Duration:</strong> ${Math.round(exec.duration_ms / 1000)}s</p>` : ''}
                ${exec.error_message ? `<p style="color: #ff4444;"><strong>Error:</strong> ${exec.error_message}</p>` : ''}
            </div>
        </div>
    `).join('');
}

async function clearHistory() {
    if (!confirm('Are you sure you want to clear all execution history? This cannot be undone.')) {
        return;
    }

    try {
        const response = await fetch(`${API_BASE}/executions`, {
            method: 'DELETE'
        });
        const data = await response.json();

        if (data.success) {
            showToast('History cleared successfully', 'success');
            loadHistory();
            loadDashboard(); // Refresh dashboard stats
        } else {
            showToast('Failed to clear history: ' + data.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

// Modal
function showModal() {
    document.getElementById('modal').classList.add('active');
}

function closeModal() {
    document.getElementById('modal').classList.remove('active');
}

// Utilities
function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

// Toast Notification System
function showToast(message, type = 'info', duration = 4000) {
    const container = document.getElementById('toast-container');

    // Create toast element
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;

    // Icon based on type
    const icons = {
        success: '✓',
        error: '✕',
        warning: '⚠',
        info: 'ℹ'
    };

    // Titles based on type
    const titles = {
        success: 'Success',
        error: 'Error',
        warning: 'Warning',
        info: 'Info'
    };

    toast.innerHTML = `
        <div class="toast-icon">${icons[type] || icons.info}</div>
        <div class="toast-content">
            <div class="toast-title">${titles[type]}</div>
            <div class="toast-message">${message}</div>
        </div>
        <button class="toast-close" onclick="closeToast(this)">&times;</button>
    `;

    container.appendChild(toast);

    // Auto dismiss after duration
    if (duration > 0) {
        setTimeout(() => {
            closeToast(toast.querySelector('.toast-close'));
        }, duration);
    }
}

function closeToast(button) {
    const toast = button.parentElement;
    toast.classList.add('hiding');

    // Remove from DOM after animation
    setTimeout(() => {
        toast.remove();
    }, 300);
}
