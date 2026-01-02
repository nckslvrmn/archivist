// History module
import { executionsAPI } from './api.js';
import { showToast, formatBytes } from './utils.js';

export function initHistory() {
    loadHistory();

    // Expose functions globally
    window.clearHistory = clearHistory;
}

export async function loadHistory() {
    try {
        const response = await executionsAPI.list(20);

        if (response.success) {
            renderHistory(response.data);
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

    const template = document.getElementById('execution-card-template');
    container.innerHTML = '';

    executions.forEach(exec => {
        const card = template.content.cloneNode(true);

        // Fill in fields
        card.querySelector('[data-field="task-name"]').textContent = exec.task_name;
        card.querySelector('[data-field="started-at"]').textContent = new Date(exec.started_at).toLocaleString();

        const badge = card.querySelector('[data-field="status-badge"]');
        badge.textContent = exec.status;
        badge.className = `badge badge-${exec.status === 'success' ? 'success' : exec.status === 'failed' ? 'error' : 'running'}`;

        // Build details HTML
        let details = '';
        if (exec.archive_size) {
            details += `<p><strong>Size:</strong> ${formatBytes(exec.archive_size)}</p>`;
        }
        if (exec.duration_ms) {
            details += `<p><strong>Duration:</strong> ${Math.round(exec.duration_ms / 1000)}s</p>`;
        }
        if (exec.error_message) {
            details += `<p style="color: #ff4444;"><strong>Error:</strong> ${exec.error_message}</p>`;
        }
        card.querySelector('[data-field="details"]').innerHTML = details;

        container.appendChild(card);
    });
}

async function clearHistory() {
    if (!confirm('Are you sure you want to clear all execution history? This cannot be undone.')) {
        return;
    }

    try {
        const response = await executionsAPI.clearHistory();

        if (response.success) {
            showToast('History cleared successfully', 'success');
            loadHistory();
        } else {
            showToast('Failed to clear history: ' + response.error.message, 'error');
        }
    } catch (error) {
        showToast('Error: ' + error.message, 'error');
    }
}
