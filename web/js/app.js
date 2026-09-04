const API_BASE = '/api';

function getToken() {
    return localStorage.getItem('token');
}

function setToken(token) {
    localStorage.setItem('token', token);
}

function clearToken() {
    localStorage.removeItem('token');
}

function getHeaders() {
    const token = getToken();
    const headers = {
        'Content-Type': 'application/json'
    };
    if (token) {
        headers['Authorization'] = `Bearer ${token}`;
    }
    return headers;
}

async function apiRequest(url, method, data) {
    try {
        const response = await fetch(API_BASE + url, {
            method: method,
            headers: getHeaders(),
            body: data ? JSON.stringify(data) : null
        });
        const result = await response.json();
        return result;
    } catch (error) {
        console.error('API request failed:', error);
        return { code: -1, message: '网络错误' };
    }
}

function isSuccess(result) {
    return result.code === 200;
}

function getMessage(result) {
    return result.message || '操作失败';
}

function getData(result) {
    if (result.data && typeof result.data === 'object') {
        const { '@type': type, ...actualData } = result.data;
        return actualData;
    }
    return result.data || {};
}

function switchTab(tab) {
    document.getElementById('login-form').classList.toggle('hidden', tab !== 'login');
    document.getElementById('register-form').classList.toggle('hidden', tab !== 'register');
    document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.textContent === (tab === 'login' ? '登录' : '注册')));
}

function showResetPassword() {
    document.getElementById('reset-password-modal').classList.remove('hidden');
}

function closeModal() {
    document.querySelectorAll('.modal').forEach(m => m.classList.add('hidden'));
}

async function sendCode(type) {
    let contact, smsType;
    if (type === 'register') {
        const registerType = document.querySelector('input[name="register-type"]:checked').value;
        if (registerType === 'phone') {
            contact = document.getElementById('register-account').value;
            smsType = 2;
        } else if (registerType === 'email') {
            contact = document.getElementById('register-account').value;
            smsType = 2;
        }
    } else {
        contact = document.getElementById('reset-contact').value;
        smsType = 3;
    }

    if (!contact) {
        alert('请输入手机号或邮箱');
        return;
    }

    const payload = smsType === 3 ? { contact: { phone: contact } } : { contact: { phone: contact }, type: smsType };
    
    const result = await apiRequest('/user/sendVerificationCode', 'POST', payload);
    if (isSuccess(result)) {
        alert('验证码已发送');
    } else {
        alert(getMessage(result));
    }
}

async function login() {
    const account = document.getElementById('login-account').value;
    const password = document.getElementById('login-password').value;

    if (!account || !password) {
        alert('请输入账号和密码');
        return;
    }

    const result = await apiRequest('/user/loginByAccount', 'POST', {
        account: account,
        password: password
    });

    if (isSuccess(result)) {
        const data = getData(result);
        setToken(data.token);
        if (data.user) {
            document.getElementById('user-name').textContent = data.user.nickname || data.user.username || '用户';
        }
        document.getElementById('auth-page').classList.add('hidden');
        document.getElementById('main-page').classList.remove('hidden');
        loadFiles();
        loadBuckets();
    } else {
        alert(getMessage(result));
    }
}

async function register() {
    const registerType = document.querySelector('input[name="register-type"]:checked').value;
    const account = document.getElementById('register-account').value;
    const name = document.getElementById('register-name').value;
    const password = document.getElementById('register-password').value;
    const code = document.getElementById('register-code').value;

    if (!account || !password) {
        alert('请输入账号和密码');
        return;
    }

    const payload = { password: password };
    
    if (registerType === 'name') {
        payload.name = account;
    } else if (registerType === 'phone') {
        payload.phone = account;
        payload.name = name || account;
        if (code) payload.smsCode = code;
    } else if (registerType === 'email') {
        payload.email = account;
        payload.name = name || account;
        if (code) payload.smsCode = code;
    }

    const result = await apiRequest('/user/register', 'POST', payload);

    if (isSuccess(result)) {
        const data = getData(result);
        setToken(data.token);
        if (data.user) {
            document.getElementById('user-name').textContent = data.user.nickname || data.user.username || '用户';
        }
        document.getElementById('auth-page').classList.add('hidden');
        document.getElementById('main-page').classList.remove('hidden');
        loadFiles();
        loadBuckets();
    } else {
        alert(getMessage(result));
    }
}

async function logout() {
    clearToken();
    document.getElementById('main-page').classList.add('hidden');
    document.getElementById('auth-page').classList.remove('hidden');
}

async function resetPassword() {
    const contact = document.getElementById('reset-contact').value;
    const code = document.getElementById('reset-code').value;
    const password = document.getElementById('reset-password').value;

    if (!contact || !code || !password) {
        alert('请填写完整信息');
        return;
    }

    const payload = {
        verificationCode: code,
        newPassword: password
    };

    if (/^1[3-9]\d{9}$/.test(contact)) {
        payload.phone = contact;
    } else {
        payload.email = contact;
    }

    const result = await apiRequest('/user/resetPassword', 'POST', payload);

    if (isSuccess(result)) {
        alert('密码重置成功');
        closeModal();
    } else {
        alert(getMessage(result));
    }
}

function showFiles() {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelector('.nav-item:first-child').classList.add('active');
    document.querySelectorAll('.panel').forEach(p => p.classList.add('hidden'));
    document.getElementById('files-panel').classList.remove('hidden');
    loadFiles();
}

function showUpload() {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelectorAll('.nav-item')[1].classList.add('active');
    document.querySelectorAll('.panel').forEach(p => p.classList.add('hidden'));
    document.getElementById('upload-panel').classList.remove('hidden');
}

function showBucket() {
    document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
    document.querySelectorAll('.nav-item')[2].classList.add('active');
    document.querySelectorAll('.panel').forEach(p => p.classList.add('hidden'));
    document.getElementById('bucket-panel').classList.remove('hidden');
    loadBuckets();
}

async function loadFiles() {
    // FileListRequest 必填参数：page、page_size、sort_by、sort_order
    const result = await apiRequest('/file/list?page=1&page_size=50&sort_by=name&sort_order=asc', 'GET');
    const fileList = document.getElementById('file-list');

    if (isSuccess(result)) {
        const data = getData(result);
        // protobuf JSON 默认 camelCase，兼容两种命名
        const files = data.files || [];
        if (files.length === 0) {
            fileList.innerHTML = '<div class="empty-state">暂无文件</div>';
            return;
        }

        fileList.innerHTML = files.map(file => {
            const fileId = file.fileId || file.file_id;
            return `
            <div class="file-item">
                <div>
                    <strong>${file.name}</strong>
                    <span style="color:#999; margin-left:10px">${formatSize(file.size)}</span>
                </div>
                <div>
                    <button class="btn btn-secondary" onclick="downloadFile('${fileId}')">下载</button>
                    <button class="btn btn-secondary" onclick="deleteFile('${fileId}')">删除</button>
                </div>
            </div>
            `;
        }).join('');
    } else {
        fileList.innerHTML = '<div class="empty-state">加载失败</div>';
    }
}

async function loadBuckets() {
    // BucketListRequest 必填参数：page、page_size
    const result = await apiRequest('/bucket/list?page=1&page_size=50', 'GET');
    const bucketList = document.getElementById('bucket-list');

    if (isSuccess(result)) {
        const data = getData(result);
        const buckets = data.buckets || [];
        if (buckets.length === 0) {
            bucketList.innerHTML = '<div class="empty-state">暂无桶</div>';
            return;
        }

        bucketList.innerHTML = buckets.map(bucket => {
            const bucketId = bucket.bucketId || bucket.bucket_id;
            return `
            <div class="bucket-item">
                <div>
                    <strong>${bucket.name}</strong>
                </div>
                <div>
                    <button class="btn btn-secondary" onclick="deleteBucket('${bucketId}')">删除</button>
                </div>
            </div>
            `;
        }).join('');
    } else {
        bucketList.innerHTML = '<div class="empty-state">加载失败</div>';
    }
}

function formatSize(bytes) {
    if (!bytes) return '0 B';
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(2) + ' MB';
}

function handleDragOver(event) {
    event.preventDefault();
}

function handleDrop(event) {
    event.preventDefault();
    const files = Array.from(event.dataTransfer.files);
    uploadFiles(files);
}

function handleFileSelect(event) {
    const files = Array.from(event.target.files);
    uploadFiles(files);
}

async function uploadFiles(files) {
    const progressContainer = document.getElementById('upload-progress');
    const progressBar = progressContainer.querySelector('.progress-bar');
    const progressText = progressContainer.querySelector('.progress-text');
    
    progressContainer.classList.remove('hidden');
    progressBar.style.width = '0%';
    progressText.textContent = '0%';

    for (let i = 0; i < files.length; i++) {
        const file = files[i];
        const formData = new FormData();
        formData.append('file', file);
        
        try {
            const response = await fetch(API_BASE + '/file/upload', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${getToken()}`
                },
                body: formData
            });
            
            const result = await response.json();
            if (!isSuccess(result)) {
                alert(`上传 ${file.name} 失败: ${getMessage(result)}`);
            }
        } catch (error) {
            alert(`上传 ${file.name} 失败: ${error.message}`);
        }
        
        const progress = ((i + 1) / files.length) * 100;
        progressBar.style.width = progress + '%';
        progressText.textContent = Math.round(progress) + '%';
    }
    
    setTimeout(() => {
        progressContainer.classList.add('hidden');
        loadFiles();
    }, 500);
}

async function downloadFile(fileId) {
    const response = await fetch(`${API_BASE}/file/download?file_id=${fileId}`, {
        headers: {
            'Authorization': `Bearer ${getToken()}`
        }
    });
    
    if (response.ok) {
        const blob = await response.blob();
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = fileId;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        window.URL.revokeObjectURL(url);
    } else {
        alert('下载失败');
    }
}

async function deleteFile(fileId) {
    if (!confirm('确定删除该文件？')) return;
    
    const result = await apiRequest('/file/delete', 'POST', { file_id: fileId });
    if (isSuccess(result)) {
        loadFiles();
    } else {
        alert(getMessage(result));
    }
}

function createBucket() {
    document.getElementById('create-bucket-modal').classList.remove('hidden');
}

async function confirmCreateBucket() {
    const name = document.getElementById('bucket-name').value;
    if (!name) {
        alert('请输入桶名称');
        return;
    }
    
    const result = await apiRequest('/bucket/create', 'POST', { name: name });
    if (isSuccess(result)) {
        closeModal();
        loadBuckets();
        document.getElementById('bucket-name').value = '';
    } else {
        alert(getMessage(result));
    }
}

async function deleteBucket(bucketId) {
    if (!confirm('确定删除该桶？')) return;
    
    const result = await apiRequest('/bucket/delete', 'POST', { bucket_id: bucketId });
    if (isSuccess(result)) {
        loadBuckets();
    } else {
        alert(getMessage(result));
    }
}

document.querySelectorAll('input[name="register-type"]').forEach(input => {
    input.addEventListener('change', function() {
        const type = this.value;
        document.getElementById('register-name-group').style.display = type === 'name' ? 'none' : 'block';
        document.getElementById('register-code-group').style.display = type === 'name' ? 'none' : 'block';
    });
});

document.addEventListener('DOMContentLoaded', function() {
    if (getToken()) {
        document.getElementById('auth-page').classList.add('hidden');
        document.getElementById('main-page').classList.remove('hidden');
        loadFiles();
        loadBuckets();
    }
});