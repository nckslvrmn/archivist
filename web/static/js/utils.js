// Utilities module

export function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

export function showToast(message, type = 'info', duration = 4000) {
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
            closeToastElement(toastElement);
        }, duration);
    }
}

function closeToastElement(toast) {
    toast.classList.add('hiding');
    setTimeout(() => {
        toast.remove();
    }, 300);
}

// Expose globally for onclick handler
window.closeToast = function(button) {
    closeToastElement(button.parentElement);
};

export function getScheduleText(schedule) {
    if (schedule.type === 'simple') {
        return schedule.simple_type.charAt(0).toUpperCase() + schedule.simple_type.slice(1);
    } else if (schedule.type === 'cron') {
        return `Cron: ${schedule.cron_expr}`;
    } else {
        return 'Manual';
    }
}
