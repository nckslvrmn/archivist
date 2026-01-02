// WebSocket module
const API_BASE = '/api/v1';
let ws = null;

export function initWebSocket(progressHandler) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}${API_BASE}/ws/progress`;

    ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        if (progressHandler) {
            progressHandler(data);
        }
    };

    ws.onclose = () => {
        console.log('WebSocket closed, reconnecting...');
        setTimeout(() => initWebSocket(progressHandler), 3000);
    };

    ws.onerror = (error) => {
        console.error('WebSocket error:', error);
    };
}
