// P8 Task 5：文件列表组件 FileList —— 分页 / 排序 / 搜索 / 下载 / 重命名 / 复制 / 删除
// 依赖 Task 3 契约：api.js（getFileList/getBucketFileList/renameFile/deleteFile/copyFile/downloadBlob）、
// store.js（store.files / store.buckets）、common.js（formatSize/formatTime/fileIcon/toast/confirmBox）
// Vue 为全量版全局变量（index.html 先以非 module 方式加载），模块内直接解构
const { ref, computed, watch, onMounted, onUnmounted } = Vue;
import {
  getFileList, getBucketFileList, renameFile, deleteFile, copyFile, downloadBlob, downloadData, getStorageStats, getPresignedURL, getMyShares, revokeShare, streamDownload, getThumb, getStorageTrend, listDeletedFiles, restoreFiles, purgeFiles,
} from '../api.js';
import { store } from '../store.js';
import { formatSize, formatTime, fileIcon, toast, confirmBox, saveBlob } from '../common.js';

// 无构建链：直接注入全局 <style>，类名加 p8- 前缀防冲突
if (!document.getElementById('p8-filelist-style')) {
  const s = document.createElement('style');
  s.id = 'p8-filelist-style';
  s.textContent = `
    .p8-toolbar { display:flex; gap:12px; flex-wrap:wrap; align-items:center; margin-bottom:20px; }
    .p8-toolbar select, .p8-toolbar input { padding:8px 10px; border:1px solid #ddd; border-radius:8px;
      font-size:14px; color:#333; background:#fff; outline:none; transition:border-color .2s; }
    .p8-toolbar select:focus, .p8-toolbar input:focus { border-color:#667eea; }
    .p8-toolbar input { flex:1; min-width:180px; }
    .p8-item-main { display:flex; align-items:center; gap:10px; flex:1; min-width:0; margin-right:15px; }
    .p8-item-icon { font-size:22px; flex-shrink:0; }
    .p8-item-name { font-weight:500; color:#333; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .p8-item-meta { color:#999; font-size:13px; white-space:nowrap; margin-right:15px; }
    .p8-actions { display:flex; gap:6px; flex-shrink:0; }
    .p8-actions button { border:1px solid #ddd; background:#fff; border-radius:6px; padding:5px 10px;
      font-size:13px; cursor:pointer; color:#555; transition:all .2s; }
    .p8-actions button:hover:not(:disabled) { border-color:#667eea; color:#667eea; }
    .p8-actions button.p8-danger:hover:not(:disabled) { border-color:#f56c6c; color:#f56c6c; }
    .p8-actions button:disabled { opacity:.45; cursor:not-allowed; }
    .p8-preview-mask { position:fixed; inset:0; background:rgba(0,0,0,.6); display:flex; align-items:center; justify-content:center; z-index:1000; }
    .p8-preview-body { background:#fff; border-radius:12px; padding:16px; max-width:86vw; max-height:88vh; display:flex; flex-direction:column; gap:10px; }
    .p8-preview-body img { max-width:80vw; max-height:70vh; object-fit:contain; border-radius:8px; }
    .p8-preview-title { display:flex; justify-content:space-between; align-items:center; font-size:14px; color:#444; }
    .p8-preview-close { border:none; background:none; font-size:18px; cursor:pointer; color:#888; }
    .p8-item-thumb { width:36px; height:36px; object-fit:cover; border-radius:6px; border:1px solid #eee; }
    .p8-share-table { width:100%; border-collapse:collapse; font-size:13px; }
    .p8-share-table th, .p8-share-table td { padding:8px 6px; border-bottom:1px solid #eee; text-align:left; word-break:break-all; }
    .p8-share-table th { color:#666; font-weight:600; }
    .p8-share-revoke { border:none; background:none; color:#e74c3c; cursor:pointer; padding:2px 6px; }
    .p8-trend-chart { display:flex; align-items:flex-end; gap:4px; height:180px; padding:10px 4px 0; }
    .p8-trend-col { flex:1; display:flex; flex-direction:column; align-items:center; justify-content:flex-end; height:100%; }
    .p8-trend-bar { width:100%; max-width:26px; background:linear-gradient(180deg,#667eea,#764ba2); border-radius:4px 4px 0 0; min-height:2px; }
    .p8-trend-col:hover .p8-trend-bar { background:#5563d8; }
    .p8-trend-tip { font-size:11px; color:#666; margin-top:4px; transform:rotate(-45deg); white-space:nowrap; }
    .p8-recycle-restore { border:none; background:none; color:#667eea; cursor:pointer; padding:2px 6px; }
    .p8-recycle-purge { border:none; background:none; color:#e74c3c; cursor:pointer; padding:2px 6px; }
    .p8-pagination { display:flex; justify-content:space-between; align-items:center; margin-top:20px;
      color:#666; font-size:14px; }
    .p8-page-btns { display:flex; gap:8px; }
    .p8-page-btns button { padding:6px 14px; border:1px solid #ddd; background:#fff; border-radius:8px;
      font-size:13px; cursor:pointer; color:#555; transition:all .2s; }
    .p8-page-btns button:hover:not(:disabled) { border-color:#667eea; color:#667eea; }
    .p8-page-btns button:disabled { opacity:.45; cursor:not-allowed; }
    .p8-skeleton { height:46px; border-radius:10px; margin-bottom:15px;
      background:linear-gradient(90deg,#f0f0f0 25%,#e8e8e8 37%,#f0f0f0 50%);
      background-size:400% 100%; animation:p8-shimmer 1.2s ease infinite; }
    @keyframes p8-shimmer { 0% { background-position:100% 0; } 100% { background-position:0 0; } }
  `;
  document.head.appendChild(s);
}

// P10：用量统计卡样式，同样以 p10- 前缀注入防冲突
if (!document.getElementById('p10-filelist-style')) {
  const s = document.createElement('style');
  s.id = 'p10-filelist-style';
  s.textContent = `
    .p10-stats-card { display:flex; align-items:center; gap:8px; padding:12px 16px; margin-bottom:16px;
      background:linear-gradient(135deg,#f5f7ff,#eef1ff); border:1px solid #e3e8ff; border-radius:10px;
      color:#555; font-size:14px; }
  `;
  document.head.appendChild(s);
}

export const FileList = {
  name: 'FileList',
  setup() {
    const F = store.files;

    // ===== 搜索（前端过滤当前页，300ms 防抖）=====
    const keyword = ref('');
    const debouncedKw = ref('');
    let kwTimer = null;
    // 搜索后端化（P14）：防抖 300ms → 回第 1 页带 name_keyword 重新查询
    watch(keyword, (v) => {
      clearTimeout(kwTimer);
      kwTimer = setTimeout(() => {
        debouncedKw.value = v.trim();
        F.page = 1;
        loadFiles();
      }, 300);
    });
    onUnmounted(() => clearTimeout(kwTimer));

    const displayList = computed(() => F.list);

    const totalPages = computed(() => Math.max(1, Math.ceil(F.total / F.pageSize)));
    const hasBuckets = computed(() => store.buckets.length > 0);

    // 桶字段兼容（gateway 序列化名以实际响应为准）
    const bucketIdOf = (b) => b.bucketId || b.bucket_id || '';
    const extOf = (f) => f.ext || f.suffix || '';

    // 桶下拉数据（复制弹层 / 桶内 scope 都需要）：Task 7 起统一走 store.loadBuckets()，本组件不再自实现
    const loadBuckets = () => store.loadBuckets();

    // ===== 列表加载（loadSeq 竞态防护：快速翻页时旧慢响应不得覆盖新数据）=====
    let loadSeq = 0;
    async function loadFiles() {
      loadStats(); // P10：统计卡随列表加载/refreshFiles 一并刷新（fire-and-forget，失败静默）
      if (F.scope === 'bucket' && !F.bucketId) { F.list = []; F.total = 0; return; }
      const seq = ++loadSeq;
      F.loading = true;
      try {
        const params = { page: F.page, page_size: F.pageSize, sort_by: F.sortBy, sort_order: F.sortOrder };
        if (debouncedKw.value) params.name_keyword = debouncedKw.value;
        const data = F.scope === 'bucket'
          ? await getBucketFileList({ ...params, bucket_id: F.bucketId })
          : await getFileList(params);
        if (seq !== loadSeq) return; // 已有更新的请求发出，丢弃本次过期响应
        F.list = data.files || [];
        F.total = data.total || 0;
        loadThumbs();
      } catch (e) {
        if (seq !== loadSeq) return; // 过期请求的失败同样不覆盖新数据/不打扰用户
        toast(e.message, 'error');
        F.list = []; F.total = 0;
      } finally {
        if (seq === loadSeq) F.loading = false;
      }
    }

    // 排序 / scope / 桶变化：回到第 1 页再请求（page 已是 1 时直接请求）
    function resetAndLoad() {
      if (F.page !== 1) F.page = 1; // page watcher 触发 loadFiles
      else loadFiles();
    }
    watch(() => F.page, () => loadFiles());
    watch(() => [F.sortBy, F.sortOrder, F.scope, F.bucketId], resetAndLoad);

    // ===== 用量统计卡（P10）：挂载与每次列表刷新后拉取；失败静默仅 console，卡片显示占位「—」 =====
    const stats = ref({ ready: false, fileCount: '—', totalSize: '—', bucketCount: '—' });
    async function loadStats() {
      try {
        const data = await getStorageStats();
        // 桶字段兼容（gateway 序列化名以实际响应为准）
        stats.value = {
          ready: true,
          fileCount: data.file_count ?? data.fileCount ?? 0,
          totalSize: data.total_size ?? data.totalSize ?? 0,
          bucketCount: data.bucket_count ?? data.bucketCount ?? 0,
        };
      } catch (e) {
        console.warn('用量统计加载失败', e); // 静默：不打扰用户，保留占位
      }
    }

    // 行操作：删除后若当前页已空则回退一页
    function shrinkPageIfEmpty() {
      if (F.list.length <= 1 && F.page > 1) F.page -= 1;
      else loadFiles();
    }

    // ===== 行操作 =====
    // ===== 图片预览 =====
    const IMAGE_EXTS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp'];
    const IMAGE_MIME = { jpg: 'image/jpeg', jpeg: 'image/jpeg', png: 'image/png', gif: 'image/gif', webp: 'image/webp', bmp: 'image/bmp' };
    const preview = ref({ show: false, url: '', name: '' });

    function isImage(f) {
      return IMAGE_EXTS.includes(extOf(f).toLowerCase());
    }

    async function openPreview(f) {
      if ((f.size || 0) > 20 * 1024 * 1024) return toast('图片超过 20MB，不支持预览', 'error');
      try {
        const data = await downloadData({ file_id: f.fileId || f.file_id });
        const mime = IMAGE_MIME[extOf(f).toLowerCase()] || 'image/jpeg';
        preview.value = { show: true, url: `data:${mime};base64,${data.content}`, name: f.name };
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    function closePreview() {
      preview.value = { show: false, url: '', name: '' };
    }

    // ===== 回收站 =====
    const recycle = ref({ show: false, list: [], total: 0, page: 1, pageSize: 10, loading: false });

    async function loadRecycle() {
      recycle.value.loading = true;
      try {
        const data = await listDeletedFiles({ page: recycle.value.page, page_size: recycle.value.pageSize });
        recycle.value.list = data.files || [];
        recycle.value.total = data.total || 0;
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        recycle.value.loading = false;
      }
    }

    function openRecycle() {
      recycle.value.page = 1;
      recycle.value.show = true;
      loadRecycle();
    }

    async function doRestore(f) {
      try {
        const r = await restoreFiles({ fileIds: [f.fileId || f.file_id] });
        toast(`已恢复 ${r.restoredCount} 个文件`, 'success');
        loadRecycle();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    async function doPurge(f) {
      if (!window.confirm(`彻底删除「${f.name}」？此操作不可恢复，容量将归还`)) return;
      try {
        const r = await purgeFiles({ fileIds: [f.fileId || f.file_id] });
        toast(`已彻底删除 ${r.purgedCount} 个文件`, 'success');
        loadRecycle();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    // ===== 用量趋势 =====
    const trend = ref({ show: false, points: [], loading: false, days: 30 });
    const maxTrendSize = computed(() => Math.max(1, ...trend.value.points.map((p) => Number(p.totalSize) || 0)));

    async function openTrend() {
      trend.value.show = true;
      trend.value.loading = true;
      try {
        const data = await getStorageTrend({ days: trend.value.days });
        trend.value.points = data.points || [];
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        trend.value.loading = false;
      }
    }

    // ===== 图片缩略图 =====
    const thumbs = ref({});
    async function loadThumbs() {
      for (const f of F.list) {
        const id = f.fileId || f.file_id;
        if (thumbs.value[id] !== undefined) continue; // 已有（含回退空串）
        if (!isImage(f)) { thumbs.value[id] = ''; continue; }
        const url = await getThumb(id);
        thumbs.value[id] = url;
      }
    }

    // ===== 我的分享管理 =====
    const myShares = ref({ show: false, list: [], total: 0, page: 1, pageSize: 10, loading: false });

    async function loadMyShares() {
      myShares.value.loading = true;
      try {
        const data = await getMyShares({ page: myShares.value.page, page_size: myShares.value.pageSize });
        myShares.value.list = data.shares || [];
        myShares.value.total = data.total || 0;
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        myShares.value.loading = false;
      }
    }

    function openMyShares() {
      myShares.value.page = 1;
      myShares.value.show = true;
      loadMyShares();
    }

    async function doRevoke(item) {
      if (!window.confirm(`确定撤销分享「${item.shareCode}」？撤销后链接立即失效`)) return;
      try {
        await revokeShare({ shareCode: item.shareCode });
        toast('分享已撤销', 'success');
        loadMyShares();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    async function doDownload(f) {
      const fileId = f.fileId || f.file_id;
      const isBig = (f.size || 0) > 20 * 1024 * 1024;
      // 1. 预签名直连（绕过网关，MinIO 驱动可用；本地驱动 40005 回落）
      try {
        const pres = await getPresignedURL({ file_id: fileId });
        if (pres && pres.url) {
          const resp = await fetch(pres.url);
          if (!resp.ok) throw { code: -1, message: `预签名下载失败（HTTP ${resp.status}）` };
          saveBlob(await resp.blob(), f.name);
          toast('文件已开始下载（直连）', 'success');
          return;
        }
      } catch (e) {
        if (e.code !== 40005) return toast(e.message, 'error');
      }
      // 2. 大文件走流式分块下载（任意驱动可用，内存只占单块）
      if (isBig) {
        try {
          const { blob } = await streamDownload(fileId, f.name);
          saveBlob(blob, f.name);
          toast('文件已开始下载（流式）', 'success');
          return;
        } catch (e) {
          return toast(e.message, 'error');
        }
      }
      // 3. 小文件普通下载（一次性 base64）
      try {
        await downloadBlob('/file/download', { file_id: fileId }, f.name);
        toast('文件已开始下载', 'success');
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    async function doRename(f) {
      const v = window.prompt('重命名文件', f.name);
      if (v === null) return;
      const newName = v.trim();
      if (!newName) return toast('文件名不能为空', 'error');
      if (newName.length > 100) return toast('文件名最长 100 个字符', 'error');
      if (newName === f.name) return;
      try {
        await renameFile({ fileId: f.fileId || f.file_id, newName });
        toast('重命名成功', 'success');
        loadFiles();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    // 复制弹层：目标桶（排除当前桶）+ 新名称（默认原名）
    const copyModal = ref({ show: false, file: null, targetBucketId: '', newName: '' });
    const targetBuckets = computed(() => {
      const file = copyModal.value.file;
      if (!file) return [];
      const cur = file.bucketId || file.bucket_id;
      return store.buckets.filter((b) => bucketIdOf(b) !== cur);
    });
    function openCopy(f) {
      if (!hasBuckets.value) return toast('暂无其他桶，请先创建桶', 'error');
      const candidates = store.buckets.filter((b) => bucketIdOf(b) !== (f.bucketId || f.bucket_id));
      if (!candidates.length) return toast('暂无其他桶可作为复制目标', 'error');
      copyModal.value = { show: true, file: f, targetBucketId: bucketIdOf(candidates[0]), newName: f.name };
    }
    async function doCopy() {
      const m = copyModal.value;
      if (!m.targetBucketId) return toast('请选择目标桶', 'error');
      const newName = (m.newName || '').trim();
      if (!newName) return toast('新文件名不能为空', 'error');
      try {
        await copyFile({ fileId: m.file.fileId || m.file.file_id, targetBucketId: m.targetBucketId, newName });
        toast('复制成功', 'success');
        m.show = false;
        loadFiles();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    async function doDelete(f) {
      const ok = await confirmBox(`确定删除文件「${f.name}」吗？删除后不可恢复。`);
      if (!ok) return;
      try {
        await deleteFile({ fileId: f.fileId || f.file_id });
        toast('删除成功', 'success');
        shrinkPageIfEmpty();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    onMounted(() => {
      loadBuckets();
      loadFiles();
      // 导出刷新方法：Task 6 上传完成后调用 store.refreshFiles() 刷新列表
      store.refreshFiles = loadFiles;
    });

    return {
      store, keyword, displayList, totalPages, hasBuckets, stats,
      formatSize, formatTime, fileIcon, extOf,
      doDownload, isImage, openPreview, closePreview, preview, myShares, openMyShares, loadMyShares, doRevoke, thumbs, loadThumbs, trend, openTrend, maxTrendSize, recycle, openRecycle, loadRecycle, doRestore, doPurge, doRename, openCopy, copyModal, targetBuckets, doCopy, doDelete,
      bucketIdOf,
    };
  },
  template: `
    <div class="panel">
      <div class="panel-header">
        <h2>我的文件</h2>
        <button class="btn btn-secondary" @click="openMyShares">我的分享</button>
        <button class="btn btn-secondary" style="margin-left:8px" @click="openTrend">用量趋势</button>
        <button class="btn btn-secondary" style="margin-left:8px" @click="openRecycle">回收站</button>
      </div>

      <!-- 统计卡（P10）：用量概览，加载失败显示占位 -->
      <div class="p10-stats-card">
        <template v-if="stats.ready">
          📁 {{ stats.fileCount }} 个文件 · 共 {{ formatSize(stats.totalSize) }} · {{ stats.bucketCount }} 个桶
        </template>
        <template v-else>📁 — 个文件 · 共 — · — 个桶</template>
      </div>

      <!-- 工具条：scope / 桶 / 排序 / 搜索 -->
      <div class="p8-toolbar">
        <select v-model="store.files.scope">
          <option value="user">全部文件</option>
          <option value="bucket">桶内文件</option>
        </select>
        <select v-if="store.files.scope==='bucket'" v-model="store.files.bucketId">
          <option value="" disabled>请选择桶</option>
          <option v-for="b in store.buckets" :key="bucketIdOf(b)" :value="bucketIdOf(b)">{{ b.name }}</option>
        </select>
        <select v-model="store.files.sortBy">
          <option value="update_time">按更新时间</option>
          <option value="create_time">按创建时间</option>
          <option value="name">按名称</option>
          <option value="size">按大小</option>
        </select>
        <select v-model="store.files.sortOrder">
          <option value="desc">降序</option>
          <option value="asc">升序</option>
        </select>
        <input v-model="keyword" type="text" placeholder="按文件名搜索">
      </div>

      <!-- 列表：loading 骨架 / 行 / 空状态 -->
      <div class="file-list">
        <template v-if="store.files.loading">
          <div class="p8-skeleton" v-for="i in 3" :key="'sk'+i"></div>
        </template>

        <template v-else-if="store.files.scope==='bucket' && !store.files.bucketId">
          <div class="empty-state">请先在上方选择一个桶</div>
        </template>

        <template v-else-if="displayList.length">
          <div class="file-item" v-for="f in displayList" :key="f.fileId || f.file_id">
            <div class="p8-item-main">
              <img v-if="thumbs[f.fileId || f.file_id]" class="p8-item-thumb" :src="thumbs[f.fileId || f.file_id]" alt="">
              <span v-else class="p8-item-icon">{{ fileIcon(extOf(f)) }}</span>
              <span class="p8-item-name" :title="f.name">{{ f.name }}</span>
            </div>
            <span class="p8-item-meta">{{ formatSize(f.size) }}</span>
            <span class="p8-item-meta">{{ formatTime(f.createdAt || f.created_at) }}</span>
            <div class="p8-actions">
              <button v-if="isImage(f)" @click="openPreview(f)">预览</button>
              <button @click="doDownload(f)">下载</button>
              <button @click="doRename(f)">重命名</button>
              <button @click="openCopy(f)">复制</button>
              <button @click="store.shareFile = f">分享</button>
              <button class="p8-danger" @click="doDelete(f)">删除</button>
            </div>
          </div>
        </template>

        <div v-else-if="store.files.scope==='bucket' && !hasBuckets" class="empty-state">
          暂无桶，请先创建桶（桶管理 Task 7 实现）
        </div>
        <div v-else class="empty-state">暂无文件，去上传一个吧</div>
      </div>

      <!-- 分页器 -->
      <div class="p8-pagination" v-if="!store.files.loading && store.files.total > 0">
        <span>共 {{ store.files.total }} 条 · 第 {{ store.files.page }}/{{ totalPages }} 页</span>
        <div class="p8-page-btns">
          <button :disabled="store.files.page <= 1" @click="store.files.page--">上一页</button>
          <button :disabled="store.files.page >= totalPages" @click="store.files.page++">下一页</button>
        </div>
      </div>

      <!-- 复制弹层：目标桶 + 新名称 -->
      <div v-if="copyModal.show" class="modal" @click.self="copyModal.show=false">
        <div class="modal-content">
          <h3>复制文件</h3>
          <div class="form-group">
            <label>源文件</label>
            <input type="text" :value="copyModal.file && copyModal.file.name" disabled>
          </div>
          <div class="form-group">
            <label>目标桶</label>
            <select v-model="copyModal.targetBucketId" style="width:100%;padding:12px;border:1px solid #ddd;border-radius:8px;font-size:16px">
              <option v-for="b in targetBuckets" :key="bucketIdOf(b)" :value="bucketIdOf(b)">{{ b.name }}</option>
            </select>
          </div>
          <div class="form-group">
            <label>新文件名</label>
            <input v-model="copyModal.newName" type="text" maxlength="100" @keyup.enter="doCopy">
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" @click="copyModal.show=false">取消</button>
            <button class="btn btn-primary" style="width:auto;flex:1" @click="doCopy">确认复制</button>
          </div>
        </div>
      </div>

      <!-- 我的分享弹层 -->
      <div v-if="myShares.show" class="modal" @click.self="myShares.show=false">
        <div class="modal-content" style="max-width:720px;width:92%">
          <h3>我的分享</h3>
          <div v-if="myShares.loading" class="empty-state">加载中…</div>
          <div v-else-if="!myShares.list.length" class="empty-state">还没有创建过分享</div>
          <table v-else class="p8-share-table">
            <thead><tr><th>提取码</th><th>文件</th><th>权限</th><th>已用/上限</th><th>过期时间</th><th></th></tr></thead>
            <tbody>
              <tr v-for="it in myShares.list" :key="it.shareCode">
                <td><strong>{{ it.shareCode }}</strong></td>
                <td>{{ it.fileName || it.fileId }}</td>
                <td>{{ it.permission === 'download' ? '下载' : '查看' }}</td>
                <td>{{ it.usedCount }} / {{ it.maxCount != null ? it.maxCount : '∞' }}</td>
                <td>{{ it.expireAt || '永久' }}</td>
                <td><button class="p8-share-revoke" @click="doRevoke(it)">撤销</button></td>
              </tr>
            </tbody>
          </table>
          <div class="modal-actions">
            <span style="margin-right:auto;color:#999;font-size:12px;align-self:center">共 {{ myShares.total }} 条</span>
            <button class="btn btn-secondary" @click="myShares.page>1 && (myShares.page--, loadMyShares())" :disabled="myShares.page<=1">上一页</button>
            <button class="btn btn-secondary" @click="myShares.page < Math.ceil(myShares.total/myShares.pageSize) && (myShares.page++, loadMyShares())" :disabled="myShares.page >= Math.ceil(myShares.total/myShares.pageSize)">下一页</button>
            <button class="btn btn-primary" @click="myShares.show=false">关闭</button>
          </div>
        </div>
      </div>

      <!-- 用量趋势弹层 -->
      <div v-if="trend.show" class="modal" @click.self="trend.show=false">
        <div class="modal-content" style="max-width:820px;width:94%">
          <h3>用量趋势（近 {{ trend.days }} 天）</h3>
          <div v-if="trend.loading" class="empty-state">加载中…</div>
          <template v-else>
            <div class="p8-trend-chart">
              <div class="p8-trend-col" v-for="p in trend.points" :key="p.date"
                   :title="p.date + '：' + p.fileCount + ' 个文件 / ' + formatSize(Number(p.totalSize)||0)">
                <div class="p8-trend-bar" :style="{ height: Math.max(2, (Number(p.totalSize)||0)/maxTrendSize*150) + 'px' }"></div>
              </div>
            </div>
            <div style="display:flex; justify-content:space-between; color:#999; font-size:12px; padding:6px 4px">
              <span>{{ trend.points[0] && trend.points[0].date }}</span>
              <span>容量（柱高按当日总容量占比）</span>
              <span>{{ trend.points[trend.points.length-1] && trend.points[trend.points.length-1].date }}</span>
            </div>
          </template>
          <div class="modal-actions">
            <button class="btn btn-primary" @click="trend.show=false">关闭</button>
          </div>
        </div>
      </div>

      <!-- 回收站弹层 -->
      <div v-if="recycle.show" class="modal" @click.self="recycle.show=false">
        <div class="modal-content" style="max-width:720px;width:92%">
          <h3>回收站</h3>
          <div v-if="recycle.loading" class="empty-state">加载中…</div>
          <div v-else-if="!recycle.list.length" class="empty-state">回收站是空的</div>
          <table v-else class="p8-share-table">
            <thead><tr><th>文件</th><th>大小</th><th>删除时间</th><th></th></tr></thead>
            <tbody>
              <tr v-for="f in recycle.list" :key="f.fileId">
                <td>{{ f.name }}</td>
                <td>{{ formatSize(Number(f.size)||0) }}</td>
                <td>{{ f.deletedAt || '-' }}</td>
                <td>
                  <button class="p8-recycle-restore" @click="doRestore(f)">恢复</button>
                  <button class="p8-recycle-purge" @click="doPurge(f)">彻底删除</button>
                </td>
              </tr>
            </tbody>
          </table>
          <div class="modal-actions">
            <span style="margin-right:auto;color:#999;font-size:12px;align-self:center">共 {{ recycle.total }} 条 · 超过 30 天将自动彻底删除</span>
            <button class="btn btn-primary" @click="recycle.show=false">关闭</button>
          </div>
        </div>
      </div>

      <!-- 图片预览弹层 -->
      <div v-if="preview.show" class="p8-preview-mask" @click.self="closePreview">
        <div class="p8-preview-body">
          <div class="p8-preview-title">
            <span>{{ preview.name }}</span>
            <button class="p8-preview-close" @click="closePreview">✕</button>
          </div>
          <img :src="preview.url" :alt="preview.name">
        </div>
      </div>
    </div>
  `,
};
