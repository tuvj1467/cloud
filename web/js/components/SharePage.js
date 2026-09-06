// P8 Task 7：分享访问页 SharePage —— 独立路由组件（route==='share'），无站点导航、无需登录
// 依赖 Task 3 / Task 1 契约：api.js（accessShare / downloadBlob）、store.js（store.shareCode 由路由解析）、common.js
//
// 【后端契约】GET /file/share/access?share_code= → {file, permission}（匿名）；
//             GET /file/share/download?share_code= → {content(base64), file}（匿名，Task 1 新增，经 downloadBlob 解码）。
// 错误码：30020 分享已过期 / 30021 次数用尽 / 30022 分享不存在 → 居中错误态。
const { ref, onMounted } = Vue;
import { accessShare, downloadBlob } from '../api.js';
import { store } from '../store.js';
import { formatSize, fileIcon, toast } from '../common.js';

// 无构建链：直接注入全局 <style>，类名加 p8- 前缀防冲突
if (!document.getElementById('p8-share-page-style')) {
  const s = document.createElement('style');
  s.id = 'p8-share-page-style';
  s.textContent = `
    .p8-sp-wrap { min-height:100vh; display:flex; justify-content:center; align-items:center; padding:20px; }
    .p8-sp-card { background:#fff; border-radius:20px; padding:50px 40px; max-width:420px; width:100%;
      box-shadow:0 15px 50px rgba(0,0,0,.25); text-align:center; }
    .p8-sp-logo { font-size:34px; color:#667eea; font-weight:700; margin-bottom:6px; }
    .p8-sp-title { color:#bbb; font-size:13px; margin-bottom:30px; letter-spacing:2px; }
    .p8-sp-icon { font-size:64px; margin-bottom:16px; }
    .p8-sp-name { font-size:18px; font-weight:600; color:#333; word-break:break-all; margin-bottom:8px; }
    .p8-sp-meta { color:#909399; font-size:14px; margin-bottom:6px; }
    .p8-sp-from { color:#c0c4cc; font-size:12px; margin-bottom:28px; }
    .p8-sp-btn { width:100%; }
    .p8-sp-btn:disabled { opacity:.6; cursor:not-allowed; transform:none; box-shadow:none; }
    .p8-sp-loading { color:#999; padding:30px 0; font-size:14px; }
    .p8-sp-err-icon { font-size:48px; color:#f56c6c; margin-bottom:16px; font-weight:700; }
    .p8-sp-err { color:#666; font-size:15px; margin-bottom:28px; }
  `;
  document.head.appendChild(s);
}

export const SharePage = {
  name: 'SharePage',
  setup() {
    const loading = ref(true);
    const file = ref(null);
    const permission = ref('');
    const errorCode = ref(0);
    const errorMsg = ref('');
    const downloading = ref(false);

    async function load() {
      loading.value = true;
      errorMsg.value = '';
      errorCode.value = 0;
      file.value = null;
      permission.value = '';
      try {
        // params 键名按网关实测：GET 请求查询串走 proto 字段名 snake_case（share_code）
        const data = await accessShare({ share_code: store.shareCode });
        file.value = data.file || null;
        permission.value = data.permission || '';
      } catch (e) {
        errorCode.value = (e && e.code) || 0;
        errorMsg.value = (e && e.message) || '加载失败，请稍后重试';
      } finally {
        loading.value = false;
      }
    }

    async function doDownload() {
      if (!file.value || downloading.value) return;
      downloading.value = true;
      try {
        await downloadBlob('/file/share/download', { share_code: store.shareCode }, file.value.name);
        toast('文件已开始下载', 'success');
      } catch (e) {
        toast(e.message, 'error');
        // 下载瞬间命中过期/次数用尽：切换到错误态（分享在页面停留期间失效的兜底）
        if ([30020, 30021, 30022].includes(e.code)) {
          errorCode.value = e.code;
          errorMsg.value = e.message;
          file.value = null;
        }
      } finally {
        downloading.value = false;
      }
    }

    onMounted(load);

    return { store, loading, file, permission, errorCode, errorMsg, downloading, doDownload, retry: load, formatSize, fileIcon };
  },
  template: `
    <div class="p8-sp-wrap">
      <div class="p8-sp-card">
        <div class="p8-sp-logo">☁ 云存储</div>
        <div class="p8-sp-title">文 件 分 享</div>

        <template v-if="loading">
          <div class="p8-sp-loading">正在加载分享信息…</div>
        </template>

        <!-- 正常态：文件图标 + 名称 + 大小 + 权限 -->
        <template v-else-if="file">
          <div class="p8-sp-icon">{{ fileIcon(file.suffix || file.ext) }}</div>
          <div class="p8-sp-name" :title="file.name">{{ file.name }}</div>
          <div class="p8-sp-meta">
            {{ formatSize(Number(file.size)) }}<template v-if="permission"> · {{ permission === 'download' ? '可下载' : '仅查看' }}</template>
          </div>
          <div class="p8-sp-from">来自云存储分享</div>
          <button class="btn btn-primary p8-sp-btn" :disabled="downloading" @click="doDownload">
            {{ downloading ? '下载中…' : '下载文件' }}
          </button>
        </template>

        <!-- 错误态：30020 过期 / 30021 次数用尽 / 30022 不存在（及其他失败） -->
        <template v-else>
          <div class="p8-sp-err-icon">✕</div>
          <div class="p8-sp-err">{{ errorMsg }}</div>
          <button class="btn btn-secondary p8-sp-btn" @click="retry">重新加载</button>
        </template>
      </div>
    </div>
  `,
};
