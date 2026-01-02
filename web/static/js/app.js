// Simplified app.js for htmx + Alpine.js
// Only handles toasts and WebSocket progress updates

// Toast notification function
window.showToast = function(message, type = 'info', duration = 4000) {
    const container = document.getElementById('toast-container');
    const template = document.getElementById('toast-template');
    const toast = template.content.cloneNode(true);

    const icons = {
        success: '✓',
        error: '✕',
        warning: '⚠',
        info: 'ℹ'
    };

    const titles = {
        success: 'Success',
        error: 'Error',
        warning: 'Warning',
        info: 'Info'
    };

    const toastElement = toast.querySelector('.toast');
    toastElement.className = `toast ${type}`;

    toast.querySelector('[data-field="icon"]').textContent = icons[type] || icons.info;
    toast.querySelector('[data-field="title"]').textContent = titles[type];
    toast.querySelector('[data-field="message"]').textContent = message;

    container.appendChild(toast);

    if (duration > 0) {
        setTimeout(() => {
            toastElement.classList.add('hiding');
            setTimeout(() => toastElement.remove(), 300);
        }, duration);
    }
};

window.closeToast = function(button) {
    const toast = button.parentElement;
    toast.classList.add('hiding');
    setTimeout(() => toast.remove(), 300);
};

// htmx event listeners for global error handling and success messages
document.body.addEventListener('htmx:responseError', (event) => {
    showToast('Request failed: ' + event.detail.error, 'error');
});

document.body.addEventListener('htmx:afterRequest', (event) => {
    const xhr = event.detail.xhr;
    if (xhr.status >= 200 && xhr.status < 300) {
        try {
            const response = JSON.parse(xhr.responseText);
            if (response.success && response.message) {
                showToast(response.message, 'success');
            }
        } catch (e) {
            // Not JSON, that's okay
        }
    }
});

// WebSocket for progress updates (if needed)
const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const wsUrl = `${protocol}//${window.location.host}/api/v1/ws/progress`;
let ws = null;

function initWebSocket() {
    ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        // Handle progress updates
        // Could dispatch custom event for Alpine.js to pick up
        document.dispatchEvent(new CustomEvent('progress-update', { detail: data }));
    };

    ws.onclose = () => {
        console.log('WebSocket closed, reconnecting...');
        setTimeout(initWebSocket, 3000);
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };
}

// Initialize WebSocket on page load
document.addEventListener('DOMContentLoaded', () => {
    initWebSocket();
    console.log('Archivist initialized with htmx + Alpine.js');
});
