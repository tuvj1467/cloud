import { store } from './store.js';

export const formatSize = (n) => {
  if (!n && n !== 0) return '-'; if (n < 1024) return n + ' B';
  if (n < 1048576) return (n / 1024).toFixed(1) + ' KB';
  if (n < 1073741824) return (n / 1048576).toFixed(1) + ' MB';
  return (n / 1073741824).toFixed(2) + ' GB';
};
export const formatTime = (ts) => ts ? new Date(ts.replace ? ts.replace(' ', 'T') : ts).toLocaleString('zh-CN', { hour12: false }) : '-';
const ICONS = { jpg:'🖼️', jpeg:'🖼️', png:'🖼️', gif:'🖼️', webp:'🖼️', mp4:'🎬', avi:'🎬', mkv:'🎬', mp3:'🎵', wav:'🎵', pdf:'📕', doc:'📄', docx:'📄', xls:'📊', xlsx:'📊', ppt:'📑', pptx:'📑', zip:'🗜️', rar:'🗜️', '7z':'🗜️', gz:'🗜️', tar:'🗜️', txt:'📃', md:'📃', html:'🌐', js:'📜', go:'📜' };
export const fileIcon = (ext) => ICONS[(ext || '').replace(/^\./, '').toLowerCase()] || '📦';

let toastTimer;
export function toast(msg, type = 'info') {
  store.toast = { show: true, msg, type };
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { store.toast.show = false; }, 3000);
}
export const confirmBox = (msg) => Promise.resolve(window.confirm(msg));
export const fmtExpire = (days) => days ? String(days * 86400) : '';

// saveBlob 触发浏览器保存（预签名直连与普通下载共用）
export function saveBlob(blob, filename) {
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = filename || 'download';
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}
