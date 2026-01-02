// Dashboard module
import { systemAPI, executionsAPI } from './api.js';
import { formatBytes } from './utils.js';

export function initDashboard() {
    loadDashboard();
}

export async function loadDashboard() {
    try {
        const [stats, executions] = await Promise.all([
            systemAPI.stats(),
            executionsAPI.list(5)
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

    const template = document.getElementById('recent-activity-card-template');
    container.innerHTML = '';

    executions.forEach(exec => {
        const card = template.content.cloneNode(true);

        // Fill in fields
        card.querySelector('[data-field="task-name"]').textContent = exec.task_name;

        const badge = card.querySelector('[data-field="status-badge"]');
        badge.textContent = exec.status;
        badge.className = `badge badge-${exec.status === 'success' ? 'success' : exec.status === 'failed' ? 'error' : 'running'}`;

        card.querySelector('[data-field="started-at"]').textContent = new Date(exec.started_at).toLocaleString();
        card.querySelector('[data-field="duration"]').textContent = exec.duration_ms ? Math.round(exec.duration_ms / 1000) + 's' : '-';

        container.appendChild(card);
    });
}

export function handleProgressEvent(event) {
    console.log('Progress event:', event);

    switch(event.type) {
        case 'execution_started':
        case 'execution_completed':
        case 'execution_failed':
            loadDashboard();
            break;
        case 'archive_progress':
        case 'upload_progress':
            // Could update a progress bar here
            break;
    }
}
