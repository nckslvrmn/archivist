// Main application entry point
import { initWebSocket } from './websocket.js';
import { initModal, closeModal } from './modal.js';
import { initDashboard, handleProgressEvent, loadDashboard } from './dashboard.js';
import { initTasks, loadTasks } from './tasks.js';
import { initBackends, loadBackends } from './backends.js';
import { initHistory, loadHistory } from './history.js';

// Make closeModal available globally for onclick handlers
window.closeModal = closeModal;

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
    switch (viewName) {
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

// Initialize app on DOM ready
document.addEventListener('DOMContentLoaded', () => {
    // Initialize core features
    initModal();
    initNavigation();
    initWebSocket(handleProgressEvent);

    // Initialize feature modules
    initDashboard();
    initTasks();
    initBackends();
    initHistory();
});
