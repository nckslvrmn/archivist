// Modal module
export function initModal() {
    // Modal is initialized via HTML, this function is here for consistency
}

export function showModal() {
    document.getElementById('modal').classList.add('active');
}

export function closeModal() {
    document.getElementById('modal').classList.remove('active');
}

export function setModalContent(html) {
    document.getElementById('modal-body').innerHTML = html;
}
