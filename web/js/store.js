const { reactive } = Vue;
// P8 Task 7：桶列表加载收敛到 store（FileList / UploadPanel / BucketPanel 共用同一数据源）
import { getBucketList } from './api.js';
import { toast } from './common.js';

// 进行中去重：并发调用共享同一 Promise（force:true 绕过，用于增删改后强制刷新）
let bucketsLoading = null;

export const store = reactive({
  route: 'login',           // login | app | share
  shareCode: '',
  shareFile: null,          // Task 7：FileList「分享」按钮置入待分享文件，根组件据此渲染 <share-dialog>
  token: localStorage.getItem('token') || '',
  // 解析容错：脏数据（非 JSON）时置 null 并清除，避免启动白屏
  user: (() => {
    try {
      return JSON.parse(localStorage.getItem('user') || 'null');
    } catch {
      localStorage.removeItem('user');
      return null;
    }
  })(),
  activeNav: 'files',       // files | upload | bucket
  buckets: [],
  // 统一拉桶列表：写回 store.buckets 并返回；失败非 silent 时 toast（silent 供轮询/后台刷新场景）
  loadBuckets(opts = {}) {
    const { silent = false, force = false } = opts;
    if (!force && bucketsLoading) return bucketsLoading;
    const p = getBucketList({ page: 1, page_size: 50 })
      .then((data) => { this.buckets = data.buckets || []; })
      .catch((e) => { if (!silent) toast((e && e.message) || '桶列表加载失败', 'error'); })
      .finally(() => { if (bucketsLoading === p) bucketsLoading = null; });
    bucketsLoading = p;
    return p;
  },
  files: { list: [], total: 0, page: 1, pageSize: 10, sortBy: 'update_time', sortOrder: 'desc', scope: 'user', bucketId: '', loading: false },
  uploads: [],              // {id,name,size,loaded,status:'uploading|done|error|canceled',progress,error,ctrl}
  toast: { show: false, msg: '', type: 'info' },
  saveAuth(token, user) {
    this.token = token; this.user = user;
    localStorage.setItem('token', token);
    localStorage.setItem('user', JSON.stringify(user));
  },
  logout() {
    this.token = ''; this.user = null;
    localStorage.removeItem('token'); localStorage.removeItem('user');
    // 重置业务状态，防止切换账号后残留上一账号的桶/文件/上传队列
    this.buckets = [];
    this.files = { list: [], total: 0, page: 1, pageSize: 10, sortBy: 'update_time', sortOrder: 'desc', scope: 'user', bucketId: '', loading: false };
    this.uploads = [];
    this.shareFile = null;
    history.replaceState(null, '', '#/login'); // 清除地址栏残留的 #/app，避免登出后 URL 与路由不一致
    this.navigate('#/login');
  },
  navigate(hash) {
    const m = hash.match(/^#\/share\/(.+)$/);
    if (m) { this.route = 'share'; this.shareCode = m[1]; return; }
    if (!this.token) { this.route = 'login'; return; }
    this.route = 'app';
    if (location.hash !== '#/app') history.replaceState(null, '', '#/app');
  }
});
