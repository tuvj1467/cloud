// P18 管理后台组件 AdminPanel：全局统计 + 用户管理表格（搜索/改角色/禁用）
// 依赖契约：api.js（adminListUsers/adminSetUserRole/adminSetUserDisabled/adminGetGlobalStats）、
// store.js（store.isAdmin 由登录后探测置位）、common.js（formatSize/formatTime/toast）
const { ref, computed, onMounted, onUnmounted } = Vue;
import {
  adminListUsers, adminSetUserRole, adminSetUserDisabled, adminGetGlobalStats,
  adminRecentAccessLogs, adminRecentOpLogs,
} from '../api.js';
import { store } from '../store.js';
import { formatSize, formatTime, toast } from '../common.js';

// 无构建链：注入组件级样式（p8- 前缀防冲突）
if (!document.getElementById('p8-admin-style')) {
  const s = document.createElement('style');
  s.id = 'p8-admin-style';
  s.textContent = `
    .p8-admin-stats { display:flex; gap:12px; margin-bottom:14px; }
    .p8-admin-card { flex:1; background:#f8f9ff; border:1px solid #e6e9f5; border-radius:10px; padding:12px 16px; }
    .p8-admin-card b { display:block; font-size:20px; color:#5563d8; }
    .p8-admin-card span { font-size:12px; color:#888; }
    .p8-admin-table { width:100%; border-collapse:collapse; font-size:13px; }
    .p8-admin-table th, .p8-admin-table td { padding:8px 6px; border-bottom:1px solid #eee; text-align:left; word-break:break-all; }
    .p8-admin-table th { color:#666; font-weight:600; }
    .p8-admin-role { font-size:12px; padding:2px 8px; border-radius:10px; }
    .p8-admin-role.admin { background:#fff3e0; color:#e67e22; }
    .p8-admin-role.user { background:#eef2ff; color:#667eea; }
    .p8-admin-bad { color:#e74c3c; font-size:12px; }
    .p8-admin-search { display:flex; gap:8px; margin-bottom:10px; }
    .p8-admin-search input { flex:1; padding:10px 12px; border:1px solid #ddd; border-radius:8px; font-size:14px; }
  `;
  document.head.appendChild(s);
}

export const AdminPanel = {
  name: 'AdminPanel',
  setup() {
    const stats = ref({ userCount: '-', fileCount: '-', totalSize: '-' });
    const list = ref({ users: [], total: 0, page: 1, pageSize: 10, keyword: '', loading: false });
    const searchKw = ref('');
    let kwTimer = null;

    async function loadUsers() {
      list.value.loading = true;
      try {
        const params = { page: list.value.page, page_size: list.value.pageSize };
        if (searchKw.value) params.keyword = searchKw.value;
        const data = await adminListUsers(params);
        list.value.users = data.users || [];
        list.value.total = data.total || 0;
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        list.value.loading = false;
      }
    }

    async function loadStats() {
      try {
        const d = await adminGetGlobalStats();
        stats.value = {
          userCount: d.userCount ?? 0,
          fileCount: d.fileCount ?? 0,
          totalSize: formatSize(Number(d.totalSize) || 0),
        };
      } catch { /* 统计卡失败静默 */ }
    }

    function openAdmin() {
      list.value.page = 1;
      loadUsers();
      loadStats();
    }

    async function doSetRole(u, role) {
      if (u.role === role) return;
      try {
        await adminSetUserRole({ userId: u.userId, role });
        toast(`已将 ${u.nickname || u.username} 设为 ${role === 'admin' ? '管理员' : '普通用户'}`, 'success');
        loadUsers();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    async function doSetDisabled(u) {
      const next = !u.disabled;
      if (next && !window.confirm(`确定禁用「${u.nickname || u.username}」？禁用后其登录与既有 token 将被拒绝`)) return;
      try {
        await adminSetUserDisabled({ userId: u.userId, disabled: next });
        toast(next ? '已禁用' : '已启用', 'success');
        loadUsers();
      } catch (e) {
        toast(e.message, 'error');
      }
    }

    // 最近管理操作审计
    const opLogs = ref([]);
    async function loadOpLogs() {
      try {
        const d = await adminRecentOpLogs({ limit: 50 });
        opLogs.value = d.logs || [];
      } catch { /* 静默 */ }
    }

    // 最近分享访问/下载审计
    const accessLogs = ref([]);
    async function loadAccessLogs() {
      try {
        const d = await adminRecentAccessLogs({ limit: 50 });
        accessLogs.value = d.logs || [];
      } catch { /* 静默：审计属辅助信息 */ }
    }

    onMounted(() => { loadUsers(); loadStats(); loadAccessLogs(); loadOpLogs(); });

    const totalPages = computed(() => Math.max(1, Math.ceil(list.value.total / list.value.pageSize)));

    function onSearch() {
      clearTimeout(kwTimer);
      kwTimer = setTimeout(() => { searchKw.value = list.value.keyword || ''; list.value.page = 1; loadUsers(); }, 300);
    }
    onUnmounted(() => clearTimeout(kwTimer));

    return {
      store, stats, list, totalPages, searchKw, onSearch,
      doSetRole, doSetDisabled, formatSize, formatTime, openAdmin, accessLogs, loadAccessLogs, opLogs, loadOpLogs,
    };
  },
  template: `
    <div>
      <div class="panel-header"><h2>管理后台</h2></div>

      <div class="p8-admin-stats">
        <div class="p8-admin-card"><b>{{ stats.userCount }}</b><span>注册用户</span></div>
        <div class="p8-admin-card"><b>{{ stats.fileCount }}</b><span>文件总数</span></div>
        <div class="p8-admin-card"><b>{{ stats.totalSize }}</b><span>总容量</span></div>
      </div>

      <div class="p8-admin-search">
        <input v-model="list.keyword" type="text" placeholder="搜索用户名/昵称/手机号" @input="onSearch">
      </div>

      <h3 style="margin:18px 0 8px;font-size:14px;color:#555">最近管理操作</h3>
      <div v-if="!opLogs.length" class="empty-state">暂无管理操作记录</div>
      <table v-else class="p8-share-table">
        <thead><tr><th>动作</th><th>目标</th><th>详情</th><th>操作者</th><th>时间</th></tr></thead>
        <tbody>
          <tr v-for="(lg, i) in opLogs" :key="'op'+i">
            <td>{{ { set_role:'设置角色', set_disabled:'禁用/启用', purge_file:'强制删除' }[lg.action] || lg.action }}</td>
            <td style="max-width:120px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" :title="lg.targetId">{{ lg.targetId }}</td>
            <td>{{ lg.detail || '-' }}</td>
            <td>{{ lg.adminId }}</td>
            <td>{{ lg.createdAt }}</td>
          </tr>
        </tbody>
      </table>

      <h3 style="margin:18px 0 8px;font-size:14px;color:#555">最近分享访问 / 下载</h3>
      <div v-if="!accessLogs.length" class="empty-state">暂无访问记录</div>
      <table v-else class="p8-share-table">
        <thead><tr><th>动作</th><th>提取码</th><th>IP</th><th>时间</th></tr></thead>
        <tbody>
          <tr v-for="(lg, i) in accessLogs" :key="i">
            <td>{{ lg.action === 'download' ? '下载' : (lg.action === 'access' ? '查看' : lg.action) }}</td>
            <td><strong>{{ lg.shareCode }}</strong></td>
            <td>{{ lg.ip || '-' }}</td>
            <td>{{ lg.createdAt }}</td>
          </tr>
        </tbody>
      </table>

      <div v-if="list.loading" class="empty-state">加载中…</div>
      <div v-else-if="!list.users.length" class="empty-state">无匹配用户</div>
      <table v-else class="p8-admin-table">
        <thead><tr>
          <th>用户</th><th>手机 / 邮箱</th><th>角色</th><th>状态</th>
          <th>创建时间</th><th></th>
        </tr></thead>
        <tbody>
          <tr v-for="u in list.users" :key="u.userId">
            <td><strong>{{ u.nickname || u.username }}</strong><br><span style="color:#999;font-size:12px">{{ u.username }}</span></td>
            <td>{{ u.phone || u.email || '-' }}</td>
            <td><span class="p8-admin-role" :class="u.role">{{ u.role === 'admin' ? '管理员' : '用户' }}</span></td>
            <td><span v-if="u.disabled" class="p8-admin-bad">已禁用</span><span v-else>正常</span></td>
            <td>{{ formatTime(u.createdAt || u.created_at) }}</td>
            <td>
              <button v-if="u.role !== 'admin'" class="btn btn-secondary" style="padding:4px 10px;font-size:12px"
                      @click="doSetRole(u, 'admin')">设为管理员</button>
              <button v-else class="btn btn-secondary" style="padding:4px 10px;font-size:12px"
                      @click="doSetRole(u, 'user')">设为用户</button>
              <button class="p8-danger" style="border:none;background:none;cursor:pointer;margin-left:6px"
                      @click="doSetDisabled(u)">{{ u.disabled ? '启用' : '禁用' }}</button>
            </td>
          </tr>
        </tbody>
      </table>

      <div style="display:flex; justify-content:space-between; align-items:center; margin-top:10px">
        <span style="color:#999; font-size:12px">共 {{ list.total }} 个用户 · 第 {{ list.page }}/{{ totalPages }} 页</span>
        <div class="p8-page-btns">
          <button @click="list.page>1 && (list.page--, loadUsers())" :disabled="list.page<=1">上一页</button>
          <button @click="list.page<totalPages && (list.page++, loadUsers())" :disabled="list.page>=totalPages">下一页</button>
        </div>
      </div>
    </div>
  `,
};
