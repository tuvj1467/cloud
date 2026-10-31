// app.js —— 根组件：路由分发（#/app 登录态 / #/share/:code 匿名分享页）与全局壳。
// 登录态探测基于 store.token + 401 事件（api.js 触发 logout），角色显隐由 AdminPanel 自判。
// P8 Task 7 已接入 BucketPanel / ShareDialog / SharePage，P8 前端闭环
const { createApp, computed, watch } = Vue;
import { store } from './store.js';
import { AuthPanel } from './components/AuthPanel.js';
import { FileList } from './components/FileList.js';
import { UploadPanel } from './components/UploadPanel.js';
import { BucketPanel } from './components/BucketPanel.js';
import { ShareDialog } from './components/ShareDialog.js';
import { SharePage } from './components/SharePage.js';
import { AdminPanel } from './components/AdminPanel.js';
import { adminGetGlobalStats } from './api.js';

const BASE_NAVS = [
  { key: 'files', label: '我的文件' },
  { key: 'upload', label: '上传' },
  { key: 'bucket', label: '桶管理' },
];

const app = createApp({
  components: { AuthPanel, FileList, UploadPanel, BucketPanel, ShareDialog, SharePage, AdminPanel },
  setup() {
    window.addEventListener('hashchange', () => store.navigate(location.hash));
    store.navigate(location.hash || '#/login');
    // 初始路由已是 app（如刷新）时立即探测管理角色
    if (store.route === 'app' && store.token) probeAdmin();
    const userName = () => (store.user ? (store.user.name || store.user.username || store.user.nickname || '用户') : '未登录');
    // 分享创建成功后刷新文件列表（FileList 的「分享次数」无展示列，刷新仅保序列/状态一致，可选通知点）
    const onShareCreated = () => { if (store.refreshFiles) store.refreshFiles(); };

    // 管理入口：登录后探测 /admin/stats（40300 = 普通用户，静默）
    const adminNavs = computed(() =>
      store.isAdmin ? [...BASE_NAVS, { key: 'admin', label: '管理后台' }] : BASE_NAVS
    );
    function probeAdmin() {
      if (store.route === 'app' && !store.isAdmin) {
        adminGetGlobalStats().then(() => { store.isAdmin = true; }).catch(() => {});
      }
    }
    watch(() => store.route, (r) => { if (r === 'app') probeAdmin(); });
    probeAdmin(); // 初始路由已是 app 时（如刷新）也要探测

    return { store, NAVS: adminNavs, userName, onShareCreated };
  },
  template: `
    <auth-panel v-if="store.route==='login'"></auth-panel>

    <!-- 分享页：独立路由组件，无站点导航（store.shareCode 由 store.navigate 从 #/share/:code 解析） -->
    <share-page v-else-if="store.route==='share'"></share-page>

    <!-- ==================== 主界面：header + sidebar + 三 panel ==================== -->
    <div v-else class="page">
      <header class="header">
        <div class="logo">☁ 云存储</div>
        <div class="user-info">
          <span>{{ userName() }}</span>
          <button class="btn btn-secondary" style="padding:8px 18px" @click="store.logout()">退出</button>
        </div>
      </header>

      <div class="main-content">
        <aside class="sidebar">
          <div v-for="n in NAVS" :key="n.key" class="nav-item"
            :class="{active: store.activeNav===n.key}" @click="store.activeNav=n.key">{{ n.label }}</div>
        </aside>

        <div class="content">
          <!-- 文件面板：Task 5 真组件 -->
          <file-list v-show="store.activeNav==='files'"></file-list>

          <!-- 上传面板：Task 6 真组件 -->
          <upload-panel v-show="store.activeNav==='upload'"></upload-panel>

          <!-- 桶面板：Task 7 真组件 -->
          <bucket-panel v-show="store.activeNav==='bucket'"></bucket-panel>

          <!-- 管理后台：仅 admin 角色可见入口 -->
          <admin-panel v-show="store.activeNav==='admin'"></admin-panel>
        </div>
      </div>
    </div>

    <!-- 分享弹层：FileList「分享」按钮置 store.shareFile 后由根组件渲染（emits: close / created） -->
    <!-- :key 按 fileId 强制重挂载：切换分享文件时重置弹层内部状态（否则停留在上一次的成功视图） -->
    <share-dialog v-if="store.route==='app' && store.shareFile" :key="store.shareFile.fileId" :file="store.shareFile"
      @close="store.shareFile = null" @created="onShareCreated"></share-dialog>

    <!-- 全局 toast（common.js 的 toast 依赖 store.toast 展示） -->
    <div v-if="store.toast.show" :style="{position:'fixed',top:'24px',right:'24px',
      zIndex:2000,padding:'10px 24px',borderRadius:'8px',color:'#fff',fontSize:'14px',maxWidth:'40%',
      boxShadow:'0 4px 16px rgba(0,0,0,.25)',whiteSpace:'pre-line',
      background: store.toast.type==='error' ? '#f56c6c' : (store.toast.type==='success' ? '#67c23a' : '#909399')}">
      {{ store.toast.msg }}
    </div>
  `
});
app.mount('#app');
