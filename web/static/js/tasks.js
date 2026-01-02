// Tasks module
import { tasksAPI, backendsAPI } from './api.js';
import { showToast, getScheduleText } from './utils.js';
import { showModal, closeModal, setModalContent } from './modal.js';
import { showFileBrowser } from './file-browser.js';
import { createFormGroup, createInput, createTextarea, createSelect, createCheckboxGroup, createFormActions } from './form-builder.js';
import { loadDashboard } from './dashboard.js';
import { renderDryRunResults } from './dry-run-results.js';

export function initTasks() {
    loadTasks();

    // Expose functions globally for onclick handlers
    window.showCreateTask = showCreateTask;
    window.executeTask = executeTask;
    window.dryRunTask = dryRunTask;
    window.editTask = editTask;
    window.deleteTask = deleteTask;
    window.updateScheduleFields = updateScheduleFields;
    window.updateRetentionVisibility = updateRetentionVisibility;
    window.updateBackupModeFields = updateBackupModeFields;
}

export async function loadTasks() {
    try {
        const response = await tasksAPI.list();

        if (response.success) {
            renderTasks(response.data);
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

    const template = document.getElementById('task-card-template');
    container.innerHTML = '';

    tasks.forEach(task => {
        const card = template.content.cloneNode(true);

        const mode = task.archive_options?.format === 'sync' ? 'sync' : 'archive';
        const modeIcon = mode === 'sync' ? '🔄' : '📦';
        const modeLabel = mode === 'sync' ? 'Sync' : 'Archive';

        // Fill in fields
        card.querySelector('[data-field="name"]').textContent = task.name;

        const badge = card.querySelector('[data-field="enabled-badge"]');
        badge.textContent = task.enabled ? 'Enabled' : 'Disabled';
        badge.className = `badge badge-${task.enabled ? 'success' : 'disabled'}`;

        card.querySelector('[data-field="description"]').textContent = task.description || '';
        card.querySelector('[data-field="mode"]').textContent = `${modeIcon} ${modeLabel}`;
        card.querySelector('[data-field="source-path"]').textContent = task.source_path;
        card.querySelector('[data-field="schedule"]').textContent = getScheduleText(task.schedule);
        card.querySelector('[data-field="backend-count"]').textContent = task.backend_ids.length;
        card.querySelector('[data-field="stats"]').textContent = task.stats ? `${task.stats.success_count} successes` : 'No executions yet';

        // Wire up actions
        const runBtn = card.querySelector('[data-action="run"]');
        runBtn.disabled = !task.enabled;
        runBtn.onclick = () => executeTask(task.id);

        card.querySelector('[data-action="dry-run"]').onclick = () => dryRunTask(task.id);
        card.querySelector('[data-action="edit"]').onclick = () => editTask(task.id);
        card.querySelector('[data-action="delete"]').onclick = () => deleteTask(task.id);

        container.appendChild(card);
    });
}

async function executeTask(taskId) {
    try {
        const response = await tasksAPI.execute(taskId);

        if (response.success) {
            showToast('Task execution started!', 'success');
            loadDashboard();
        } else {
            showToast('Failed to execute task: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function dryRunTask(taskId) {
    try {
        setModalContent('<div style="text-align: center; padding: 2rem;"><div style="font-size: 2rem;">⏳</div><p>Analyzing files and backends...</p></div>');
        showModal();

        const response = await tasksAPI.execute(taskId, true);

        if (response.success) {
            showDryRunResults(response.data);
        } else {
            closeModal();
            showToast('Failed to run dry run: ' + response.error.message, 'error');
        }
    } catch (error) {
        closeModal();
        showToast('Error: ' + error.message, 'error');
    }
}

function showDryRunResults(result) {
    setModalContent(renderDryRunResults(result));
}

async function showCreateTask() {
    const response = await backendsAPI.list();
    const backends = response.success ? response.data : [];

    const html = `
        <h2>Create New Task</h2>
        <form id="create-task-form">
            ${createFormGroup('Task Name *', createInput('name', '', '', 'text', true))}
            ${createFormGroup('Description', createTextarea('description', '', 3))}
            ${createFormGroup('Source Path *', `
                <div style="display: flex; gap: 0.5rem;">
                    ${createInput('source_path', '', '/data/sources/...', 'text', true).replace('>', ' id="create-source-path" style="flex: 1;">')}
                    <button type="button" class="btn btn-sm" onclick="showFileBrowser('create-source-path')" style="white-space: nowrap;">📁 Browse</button>
                </div>
            `)}
            ${createFormGroup('Storage Backend(s) *', backends.length > 0 ? `
                <div class="backend-selector">
                    ${createCheckboxGroup('backend', backends, [])}
                </div>
            ` : '<p style="color: #ff4444;">No backends configured. Please create a backend first.</p>')}
            ${createFormGroup('Schedule Type *', createSelect('schedule_type', [
                { value: 'simple', label: 'Simple' },
                { value: 'cron', label: 'Cron' },
                { value: 'manual', label: 'Manual' }
            ], 'simple', 'updateScheduleFields(this.value)'))}
            <div class="form-group" id="simple-schedule">
                <label>Frequency</label>
                ${createSelect('simple_type', ['hourly', 'daily', 'weekly', 'monthly'], 'daily')}
            </div>
            <div class="form-group" id="cron-schedule" style="display: none;">
                <label>Cron Expression</label>
                ${createInput('cron_expr', '', '0 2 * * *')}
            </div>
            ${createFormGroup('Backup Mode *', createSelect('backup_mode', [
                { value: 'archive', label: 'Archive (Compressed, point-in-time or mirror)' },
                { value: 'sync', label: 'Sync (File-by-file, for large directories)' }
            ], 'archive', 'updateBackupModeFields(this.value)'), 'Archive: Creates compressed archive. Sync: Uploads files individually')}
            <div id="archive-options">
                ${createFormGroup('Use Timestamp in Filename', createSelect('use_timestamp', [
                    { value: 'true', label: 'Yes (Point-in-time backups)' },
                    { value: 'false', label: 'No (Mirror/overwrite)' }
                ], 'true', 'updateRetentionVisibility(this.value)'))}
                ${createFormGroup('Retention (Keep Last N Backups, 0 = unlimited)', createInput('keep_last', '7', '', 'number'), 'Only applies to point-in-time backups').replace('<div class="form-group">', '<div class="form-group" id="retention-field">')}
            </div>
            <div id="sync-options" style="display: none;">
                ${createFormGroup('Delete Remote Files', createSelect('delete_remote', [
                    { value: 'false', label: 'No (Safer - only add/update files)' },
                    { value: 'true', label: 'Yes (True mirror - delete remote files not in source)' }
                ], 'false'), 'Files are compared by modification time and size<br><small style="color: #ff4444; display: block; margin-top: 0.25rem;">Warning: True mirror will delete remote files that don\'t exist locally</small>')}
            </div>
            ${createFormGroup('Initial Status', createSelect('enabled', [
                { value: 'false', label: 'Disabled (Safer - enable after reviewing)' },
                { value: 'true', label: 'Enabled (Will run on schedule)' }
            ], 'false'), 'New tasks are disabled by default for safety')}
            ${createFormActions('Cancel', 'Create Task')}
        </form>
    `;

    setModalContent(html);
    document.getElementById('create-task-form').addEventListener('submit', handleCreateTask);
    showModal();
}

async function handleCreateTask(e) {
    e.preventDefault();
    const form = e.target;
    const formData = new FormData(form);

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
                delete_remote: formData.get('delete_remote') === 'true'
            }
        } : {
            format: 'tar.gz',
            compression: 'gzip',
            use_timestamp: formData.get('use_timestamp') === 'true',
            sync_options: {
                delete_remote: false
            }
        },
        retention_policy: {
            keep_last: backupMode === 'sync' ? 0 : parseInt(formData.get('keep_last'))
        },
        enabled: formData.get('enabled') === 'true'
    };

    try {
        const response = await tasksAPI.create(task);

        if (response.success) {
            closeModal();
            loadTasks();
            showToast('Task created successfully', 'success');
        } else {
            showToast('Failed to create task: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}

async function editTask(taskId) {
    try {
        const [taskResponse, backendsResponse] = await Promise.all([
            tasksAPI.get(taskId),
            backendsAPI.list()
        ]);

        if (!taskResponse.success) {
            showToast('Failed to load task', 'error');
            return;
        }

        const task = taskResponse.data;
        const backends = backendsResponse.success ? backendsResponse.data : [];

        const currentMode = task.archive_options?.format === 'sync' ? 'sync' : 'archive';

        setModalContent(`
            <h2>Edit Task</h2>
            <form id="edit-task-form">
                ${createFormGroup('Task Name *', createInput('name', task.name, '', 'text', true))}
                ${createFormGroup('Description', createTextarea('description', task.description || '', 3))}
                ${createFormGroup('Source Path *', `
                    <div style="display: flex; gap: 0.5rem;">
                        ${createInput('source_path', task.source_path, '/data/sources/...', 'text', true).replace('>', ' id="edit-source-path" style="flex: 1;">')}
                        <button type="button" class="btn btn-sm" onclick="showFileBrowser('edit-source-path')" style="white-space: nowrap;">📁 Browse</button>
                    </div>
                `)}
                ${createFormGroup('Storage Backend(s) *', backends.length > 0 ? `
                    <div class="backend-selector">
                        ${createCheckboxGroup('backend', backends, task.backend_ids)}
                    </div>
                ` : '<p style="color: #ff4444;">No backends configured.</p>')}
                ${createFormGroup('Schedule Type *', createSelect('schedule_type', [
                    { value: 'simple', label: 'Simple' },
                    { value: 'cron', label: 'Cron' },
                    { value: 'manual', label: 'Manual' }
                ], task.schedule.type, 'updateScheduleFields(this.value)'))}
                <div class="form-group" id="simple-schedule" style="display: ${task.schedule.type === 'simple' ? 'block' : 'none'};">
                    <label>Frequency</label>
                    ${createSelect('simple_type', ['hourly', 'daily', 'weekly', 'monthly'], task.schedule.simple_type || 'daily')}
                </div>
                <div class="form-group" id="cron-schedule" style="display: ${task.schedule.type === 'cron' ? 'block' : 'none'};">
                    <label>Cron Expression</label>
                    ${createInput('cron_expr', task.schedule.cron_expr || '', '0 2 * * *')}
                </div>
                ${createFormGroup('Backup Mode *', createSelect('backup_mode', [
                    { value: 'archive', label: 'Archive (Compressed, point-in-time or mirror)' },
                    { value: 'sync', label: 'Sync (File-by-file, for large directories)' }
                ], currentMode, 'updateBackupModeFields(this.value)'), 'Archive: Creates compressed archive. Sync: Uploads files individually')}
                <div id="archive-options" style="display: ${currentMode === 'archive' ? 'block' : 'none'};">
                    ${createFormGroup('Use Timestamp in Filename', createSelect('use_timestamp', [
                        { value: 'true', label: 'Yes (Point-in-time backups)' },
                        { value: 'false', label: 'No (Mirror/overwrite)' }
                    ], task.archive_options.use_timestamp ? 'true' : 'false', 'updateRetentionVisibility(this.value)'))}
                    ${createFormGroup('Retention (Keep Last N Backups, 0 = unlimited)', createInput('keep_last', task.retention_policy.keep_last.toString(), '', 'number'), 'Only applies to point-in-time backups').replace('<div class="form-group">', '<div class="form-group" id="retention-field" style="display: ' + (task.archive_options.use_timestamp ? 'block' : 'none') + ';">')}
                </div>
                <div id="sync-options" style="display: ${currentMode === 'sync' ? 'block' : 'none'};">
                    ${createFormGroup('Delete Remote Files', createSelect('delete_remote', [
                        { value: 'false', label: 'No (Safer - only add/update files)' },
                        { value: 'true', label: 'Yes (True mirror - delete remote files not in source)' }
                    ], task.archive_options.sync_options?.delete_remote ? 'true' : 'false'), 'Files are compared by modification time and size<br><small style="color: #ff4444; display: block; margin-top: 0.25rem;">Warning: True mirror will delete remote files that don\'t exist locally</small>')}
                </div>
                ${createFormGroup('Status', createSelect('enabled', [
                    { value: 'true', label: 'Enabled' },
                    { value: 'false', label: 'Disabled' }
                ], task.enabled ? 'true' : 'false'))}
                ${createFormActions('Cancel', 'Save Changes')}
            </form>
        `);

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
                delete_remote: formData.get('delete_remote') === 'true'
            }
        } : {
            format: 'tar.gz',
            compression: 'gzip',
            use_timestamp: formData.get('use_timestamp') === 'true',
            sync_options: {
                delete_remote: false
            }
        },
        retention_policy: {
            keep_last: backupMode === 'sync' ? 0 : parseInt(formData.get('keep_last'))
        },
        enabled: formData.get('enabled') === 'true'
    };

    try {
        const response = await tasksAPI.update(taskId, task);

        if (response.success) {
            closeModal();
            loadTasks();
            showToast('Task updated successfully', 'success');
        } else {
            showToast('Failed to update task: ' + response.error.message, 'error');
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
        const response = await tasksAPI.delete(taskId);

        if (response.success) {
            loadTasks();
            showToast('Task deleted successfully', 'success');
        } else {
            showToast('Failed to delete task: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
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
