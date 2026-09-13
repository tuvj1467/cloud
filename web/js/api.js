import { store } from './store.js';

const API_BASE = '/api';
const stripType = (d) => { if (d && typeof d === 'object') { const { '@type': t, ...rest } = d; return rest; } return d || {}; };

async function request(path, { method = 'POST', params, body } = {}) {
  const url = API_BASE + path + (params ? '?' + new URLSearchParams(params) : '');
  let resp;
  try {
    resp = await fetch(url, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(store.token ? { Authorization: `Bearer ${store.token}` } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : null,
    });
  } catch { throw { code: -1, message: '网络错误，请检查服务是否启动' }; }
  // 网关对无效 token 返回 HTTP 401 + text/plain（非 JSON），必须按状态码拦截
  if (resp.status === 401) {
    if (store.route === 'app') store.logout();
    throw { code: 401, message: '登录已失效，请重新登录' };
  }
  const result = await resp.json().catch(() => { throw { code: -1, message: '响应解析失败' }; });
  if (result.code === 200) return stripType(result.data);
  if ([10101, 10102, 10103].includes(result.code) && store.route === 'app') store.logout();
  throw { code: result.code, message: errText(result.code, result.message) };
}

export const errText = (code, msg) => ({
  10001: '用户不存在', 10002: '密码错误', 10003: '账号已被注册',
  10004: '验证码错误', 10005: '验证码已过期', 10006: '发送过于频繁，请稍后再试', 10007: '验证码服务不可用',
  10101: '登录已失效', 10102: '登录已过期', 10103: '请先登录',
  20001: '桶不存在', 20002: '桶名已存在', 20003: '无权操作该桶', 20004: '桶容量不足', 20005: '桶非空，删除需强制模式',
  30001: '文件不存在', 30002: '无权操作该文件', 30003: '同桶内文件名重复', 30010: '分片不完整', 30012: '文件超过大小上限',
  30020: '分享已过期', 30021: '分享访问次数已用尽', 30022: '分享不存在',
  40002: '文件写入失败', 40003: '文件读取失败', 40005: '当前存储驱动不支持该操作', 40006: '文件名/路径非法',
  [-1]: '网络错误',
  16: '登录已失效，请重新登录',
}[code] || msg || '操作失败');

// ===== 用户 =====
export const register = (body) => request('/user/register', { body });
export const loginByAccount = (body) => request('/user/loginByAccount', { body });
export const loginByPhoneSms = (body) => request('/user/loginByPhoneSms', { body });
export const loginByEmailSms = (body) => request('/user/loginByEmailSms', { body });
export const sendVerificationCode = (body) => request('/user/sendVerificationCode', { body });
export const resetPassword = (body) => request('/user/resetPassword', { body });
export const findAccount = (body) => request('/user/findAccount', { body });
// ===== 桶 =====
export const createBucket = (body) => request('/bucket/create', { body });
export const getBucketList = (params) => request('/bucket/list', { method: 'GET', params });
export const getBucketInfo = (params) => request('/bucket/info', { method: 'GET', params });
export const updateBucket = (body) => request('/bucket/update', { body });
export const deleteBucket = (body) => request('/bucket/delete', { body });
// ===== 文件 =====
export const getFileInfo = (params) => request('/file/info', { method: 'GET', params });
export const getFileList = (params) => request('/file/list', { method: 'GET', params });
export const getBucketFileList = (params) => request('/file/bucket_list', { method: 'GET', params });
export const renameFile = (body) => request('/file/rename', { body });
export const deleteFile = (body) => request('/file/delete', { body });
export const copyFile = (body) => request('/file/copy', { body });
export const uploadFile = (body) => request('/file/upload', { body });
export const initMultipartUpload = (body) => request('/file/multipart/init', { body });
export const uploadPart = (body) => request('/file/multipart/upload', { body });
export const completeMultipartUpload = (body) => request('/file/multipart/complete', { body });
export const abortMultipartUpload = (body) => request('/file/multipart/abort', { body });
export const createShare = (body) => request('/file/share/create', { body });
export const accessShare = (params) => request('/file/share/access', { method: 'GET', params });
export const downloadByShare = (params) => request('/file/share/download', { method: 'GET', params });
export const getPresignedURL = (params) => request('/file/presign', { method: 'GET', params });
export const getStorageTrend = (params) => request('/file/stats/trend', { method: 'GET', params });
// 图片缩略图（JPEG dataURL，带缓存）；失败返回空串由调用方回退图标
const thumbCache = new Map();
export async function getThumb(fileId) {
  if (thumbCache.has(fileId)) return thumbCache.get(fileId);
  try {
    const data = await request('/file/thumb', { method: 'GET', params: { file_id: fileId } });
    const url = `data:image/jpeg;base64,${data.content}`;
    thumbCache.set(fileId, url);
    return url;
  } catch {
    thumbCache.set(fileId, '');
    return '';
  }
}
export const getMyShares = (params) => request('/file/share/my', { method: 'GET', params });
export const revokeShare = (body) => request('/file/share/revoke', { body });
export const listDeletedFiles = (params) => request('/file/recycle/list', { method: 'GET', params });
export const restoreFiles = (body) => request('/file/recycle/restore', { body });
export const purgeFiles = (body) => request('/file/recycle/purge', { body });
export const getShareAccessLogs = (params) => request('/file/share/logs', { method: 'GET', params });
export const adminListUsers = (params) => request('/admin/users', { method: 'GET', params });
export const adminSetUserRole = (body) => request('/admin/users/role', { body });
export const adminSetUserDisabled = (body) => request('/admin/users/disabled', { body });
export const adminGetGlobalStats = () => request('/admin/stats', { method: 'GET' });

// 流式下载：POST /file/download_stream（server-streaming）。
// 线格式为串联 JSON（每个 64KB 块一个 DownloadChunk 对象），
// 逐块 base64 解码拼接；onProgress(sent, total) 用于进度回调。
export async function streamDownload(fileId, name, onProgress) {
  const resp = await fetch(API_BASE + '/file/download_stream', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${store.token}` },
    body: JSON.stringify({ file_id: fileId }),
  });
  if (!resp.ok) {
    let msg = `下载失败（HTTP ${resp.status}）`;
    try { const j = await resp.json(); msg = errText(j.code, j.message); } catch { /* 流式错误可能无响应体 */ }
    throw { code: -1, message: msg };
  }
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  const parts = [];
  let buf = '', sent = 0, total = 0;
  const b64ToBytes = (b64) => {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return bytes;
  };
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let end;
    // 串联 JSON 无嵌套：以 '}{ ' 边界切出完整对象
    while ((end = buf.indexOf('}{')) >= 0) {
      const obj = JSON.parse(buf.slice(0, end + 1));
      buf = buf.slice(end + 1);
      parts.push(b64ToBytes(obj.data));
      sent = obj.sent; total = obj.total;
      if (onProgress) onProgress(sent, total);
    }
  }
  if (buf.trim()) {
    const obj = JSON.parse(buf);
    parts.push(b64ToBytes(obj.data));
    sent = obj.sent; total = obj.total;
    if (onProgress) onProgress(sent, total);
  }
  return { blob: new Blob(parts), name, sent, total };
}
export const getStorageStats = (params) => request('/file/stats', { method: 'GET', params });

// 下载：bytes 在 JSON 中为 base64，需解码
// 下载数据：返回原始响应 data（content 为 base64、file 元信息），供预览等场景使用
export const downloadData = (params) => request('/file/download', { method: 'GET', params });

export async function downloadBlob(url, params, filename) {
  const data = await request(url, { method: 'GET', params });
  const bin = atob(data.content);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const blob = new Blob([bytes]);
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = filename || (data.file && data.file.name) || 'download';
  a.click();
  URL.revokeObjectURL(a.href);
}
