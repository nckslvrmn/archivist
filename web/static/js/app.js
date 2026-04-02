// app.js — data send/receive and basic logic only
// Toast creation is handled by Alpine.js (see #toast-container in index.html)

// ── HTMX global error handling ────────────────────────────────────────────
document.body.addEventListener('htmx:responseError', (event) => {
    window.showToast?.('Request failed: ' + event.detail.error, 'error');
});

document.body.addEventListener('htmx:afterRequest', (event) => {
    const xhr = event.detail.xhr;
    if (xhr.status >= 200 && xhr.status < 300) {
        try {
            const response = JSON.parse(xhr.responseText);
            if (response.success && response.message) {
                window.showToast?.(response.message, 'success');
            }
        } catch {
            // Not JSON — that's fine
        }
    }
});

// ── HTMX progress indicator ───────────────────────────────────────────────
document.body.addEventListener('htmx:beforeRequest', () => {
    document.getElementById('htmx-indicator')?.classList.add('htmx-request');
});

document.body.addEventListener('htmx:afterRequest', () => {
    document.getElementById('htmx-indicator')?.classList.remove('htmx-request');
});

// ── WebSocket for progress updates ────────────────────────────────────────
const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const wsUrl = `${wsProtocol}//${window.location.host}/api/v1/ws/progress`;
let ws = null;

function initWebSocket() {
    ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        document.dispatchEvent(new CustomEvent('progress-update', { detail: data }));
    };

    ws.onclose = () => {
        setTimeout(initWebSocket, 3000);
    };

    ws.onerror = () => {
        // Reconnect handled by onclose
    };
}

document.addEventListener('DOMContentLoaded', () => {
    initWebSocket();
});
