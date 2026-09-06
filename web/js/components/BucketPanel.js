// P8 Task 7：桶管理组件 BucketPanel —— 列表（可见性徽标/用量条/创建时间）+ 创建/编辑/删除
// 依赖 Task 3 契约：api.js（createBucket/getBucketInfo/updateBucket/deleteBucket）、
// store.js（store.buckets / store.loadBuckets() 统一桶数据源 / store.refreshFiles）、common.js（formatSize/formatTime/toast/confirmBox）
//
// 【curl 实测事实（Task 3/5/6 已确认）】
// 1. BucketMeta 字段为 protojson camelCase：bucketId/name/description/sizeLimit/usedSize/visibility/createdAt/updatedAt；
//    sizeLimit/usedSize 为字符串（proto int64）——展示与比例计算一律 Number() 后处理。
// 2. /bucket/update 为 optional 字段语义：仅传修改项（不传 = 不修改，后端保持默认值语义），故保存前做 diff。
// 3. /bucket/delete 非空桶返回 20005「桶非空，删除需强制模式」→ 二次确认后 force:true 级联清空。
const { ref, computed, onMounted } = Vue;
import { createBucket, getBucketInfo, updateBucket, deleteBucket } from '../api.js';
import { store } from '../store.js';
import { formatSize, formatTime, toast, confirmBox } from '../common.js';

// 无构建链：直接注入全局 <style>，类名加 p8- 前缀防冲突（p8-actions 复用 FileList 注入的全局样式）
if (!document.getElementById('p8-bucket-style')) {
  const s = document.createElement('style');
  s.id = 'p8-bucket-style';
  s.textContent = `
    .p8-bk-create { width:auto; padding:10px 20px; }
    .p8-bk-item { display:flex; justify-content:space-between; align-items:center; gap:15px; }
    .p8-bk-main { flex:1; min-width:0; }
    .p8-bk-name-row { display:flex; align-items:center; gap:10px; }
    .p8-bk-name { font-weight:600; color:#333; font-size:15px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .p8-badge { padding:2px 10px; border-radius:10px; font-size:12px; flex-shrink:0; }
    .p8-badge.private { background:#f4f4f5; color:#909399; }
    .p8-badge.internal { background:#fdf6ec; color:#e6a23c; }
    .p8-badge.public { background:#f0f9eb; color:#67c23a; }
    .p8-bk-desc { color:#888; font-size:13px; margin-top:4px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
    .p8-bk-usage { display:flex; align-items:center; gap:10px; margin-top:8px; }
    .p8-bk-barwrap { flex:1; max-width:320px; height:10px; background:#ececec; border-radius:5px; overflow:hidden; }
    .p8-bk-bar { height:100%; background:linear-gradient(135deg,#667eea 0%,#764ba2 100%); width:0%; transition:width .3s; }
    .p8-bk-bar.danger { background:#f56c6c; }
    .p8-bk-usage-text { font-size:12px; color:#909399; white-space:nowrap; flex-shrink:0; }
    .p8-bk-usage-text.danger { color:#f56c6c; }
    .p8-bk-time { color:#bbb; font-size:12px; margin-top:6px; }
    .p8-bk-select { width:100%; padding:12px; border:1px solid #ddd; border-radius:8px; font-size:16px; background:#fff; outline:none; transition:border-color .3s; }
    .p8-bk-select:focus { border-color:#667eea; }
    .p8-bk-textarea { width:100%; padding:12px; border:1px solid #ddd; border-radius:8px; font-size:14px; resize:vertical; min-height:70px; font-family:inherit; outline:none; transition:border-color .3s; }
    .p8-bk-textarea:focus { border-color:#667eea; }
    .p8-bk-hint { color:#bbb; font-size:12px; margin-top:4px; }
  `;
  document.head.appendChild(s);
}

const GB = 1073741824; // 字节换算基数（sizeLimit 单位字节，输入单位 GB）
const VIS_LABELS = { private: '私有', internal: '内部', public: '公开' };

export const BucketPanel = {
  name: 'BucketPanel',
  setup() {
    const bucketIdOf = (b) => b.bucketId || b.bucket_id || '';
    const visText = (v) => VIS_LABELS[v] || v || '-';
    const usedPct = (b) => {
      const limit = Number(b.sizeLimit) || 0;
      if (limit <= 0) return 0;
      return Math.min(100, (Number(b.usedSize) || 0) / limit * 100);
    };

    // ===== 创建弹层 =====
    // 后端实测（2026-09-06）：CreateBucketRequest.visibility 带 in-list 校验，空串报 50000 参数校验失败
    // —— 表单显式提供可见性下拉（默认 private），不存在「不传」路径
    const createModal = ref({ show: false, name: '', description: '', visibility: 'private', busy: false });
    function openCreate() { createModal.value = { show: true, name: '', description: '', visibility: 'private', busy: false }; }
    async function doCreate() {
      const m = createModal.value;
      const name = (m.name || '').trim();
      if (!name) return toast('桶名称不能为空', 'error');
      if (name.length > 63) return toast('桶名称不能超过 63 字符', 'error');
      m.busy = true;
      try {
        const desc = (m.description || '').trim();
        const body = { name, visibility: m.visibility };
        if (desc) body.description = desc;
        await createBucket(body);
        toast(`桶「${name}」创建成功`, 'success');
        m.show = false;
        store.loadBuckets({ force: true });
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        m.busy = false;
      }
    }

    // ===== 编辑弹层（optional 字段仅传修改项：保存前与打开时快照做 diff）=====
    const editModal = ref({ show: false, busy: false, bucketId: '', name: '', description: '', sizeLimitGb: '', visibility: 'private', orig: null });
    async function openEdit(b) {
      const bid = bucketIdOf(b);
      // 打开时拉详情兜底（列表数据通常已够；失败则退回列表行数据）
      let cur = b;
      try {
        const info = await getBucketInfo({ bucket_id: bid });
        if (info && info.bucket) cur = info.bucket;
      } catch { /* 用列表行数据继续 */ }
      editModal.value = {
        show: true, busy: false, bucketId: bid, name: cur.name || '',
        description: cur.description || '',
        sizeLimitGb: cur.sizeLimit ? String(Number(cur.sizeLimit) / GB) : '',
        visibility: cur.visibility || 'private',
        orig: { description: cur.description || '', sizeLimit: Number(cur.sizeLimit) || 0, visibility: cur.visibility || 'private' },
      };
    }
    async function doUpdate() {
      const m = editModal.value;
      const body = { bucketId: m.bucketId };
      const desc = (m.description || '').trim();
      if (desc !== m.orig.description) body.description = desc;
      const gb = parseFloat(m.sizeLimitGb);
      const bytes = Math.round(gb * GB);
      if (!m.sizeLimitGb.trim() || Number.isNaN(gb) || gb <= 0) {
        return toast('容量上限必须为正数（GB）', 'error');
      }
      if (bytes !== m.orig.sizeLimit) body.sizeLimit = bytes;
      if (m.visibility !== m.orig.visibility) body.visibility = m.visibility;
      if (body.description === undefined && body.sizeLimit === undefined && body.visibility === undefined) {
        m.show = false;
        return; // 无修改项：optional 语义下不发请求
      }
      m.busy = true;
      try {
        await updateBucket(body);
        toast('桶已更新', 'success');
        m.show = false;
        store.loadBuckets({ force: true });
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        m.busy = false;
      }
    }

    // ===== 删除（两级确认：非空 20005 → 强制删除危害提示 → force:true）=====
    async function doDelete(b) {
      const bid = bucketIdOf(b);
      const name = b.name;
      const ok = await confirmBox(`确定删除桶「${name}」吗？`);
      if (!ok) return;
      try {
        await deleteBucket({ bucketId: bid, force: false });
        toast(`桶「${name}」已删除`, 'success');
        afterBucketGone(bid);
      } catch (e) {
        // 后端实测（2026-09-06）：delete_bucket_logic.go 对非空桶返回裸 errors.New（网关映射为 50000），
        // 未使用 CodeBucketNotEmpty(20005)；前端双信号匹配（20005 或消息含「非空」）保证两级确认可用
        const nonEmpty = e.code === 20005 || (e.message && e.message.includes('非空'));
        if (nonEmpty) {
          const force = await confirmBox(`桶「${name}」非空。强制删除将清空桶内全部文件且不可恢复，确定继续吗？`);
          if (!force) return;
          try {
            await deleteBucket({ bucketId: bid, force: true });
            toast(`桶「${name}」已强制删除（含桶内文件）`, 'success');
            afterBucketGone(bid);
          } catch (e2) {
            toast(e2.message, 'error');
          }
        } else {
          toast(e.message, 'error');
        }
      }
    }
    // 删除成功后：刷新桶列表；若被删桶是文件面板当前选中桶则重置为「全部文件」并刷新列表
    function afterBucketGone(bid) {
      store.loadBuckets({ force: true });
      if (store.files.scope === 'bucket' && store.files.bucketId === bid) {
        store.files.scope = 'user';
        store.files.bucketId = '';
      }
      if (store.refreshFiles) store.refreshFiles();
    }

    const hasBuckets = computed(() => store.buckets.length > 0);

    onMounted(() => { store.loadBuckets(); });

    return {
      store, bucketIdOf, visText, usedPct, hasBuckets,
      createModal, openCreate, doCreate,
      editModal, openEdit, doUpdate, doDelete,
      formatSize, formatTime,
    };
  },
  template: `
    <div class="panel">
      <div class="panel-header">
        <h2>桶管理</h2>
        <button class="btn btn-primary p8-bk-create" @click="openCreate">+ 创建桶</button>
      </div>

      <!-- 桶列表：名称/可见性徽标/用量条/创建时间/操作 -->
      <div class="bucket-list" v-if="hasBuckets">
        <div class="bucket-item p8-bk-item" v-for="b in store.buckets" :key="bucketIdOf(b)">
          <div class="p8-bk-main">
            <div class="p8-bk-name-row">
              <span class="p8-bk-name" :title="b.name">{{ b.name }}</span>
              <span class="p8-badge" :class="b.visibility">{{ visText(b.visibility) }}</span>
            </div>
            <div class="p8-bk-desc" v-if="b.description" :title="b.description">{{ b.description }}</div>
            <div class="p8-bk-usage">
              <div class="p8-bk-barwrap">
                <div class="p8-bk-bar" :class="{danger: usedPct(b) > 80}" :style="{width: usedPct(b) + '%'}"></div>
              </div>
              <span class="p8-bk-usage-text" :class="{danger: usedPct(b) > 80}">
                {{ formatSize(Number(b.usedSize)) }} / {{ formatSize(Number(b.sizeLimit)) }}（{{ usedPct(b).toFixed(1) }}%）
              </span>
            </div>
            <div class="p8-bk-time">创建于 {{ formatTime(b.createdAt || b.created_at) }}</div>
          </div>
          <div class="p8-actions">
            <button @click="openEdit(b)">编辑</button>
            <button class="p8-danger" @click="doDelete(b)">删除</button>
          </div>
        </div>
      </div>
      <div v-else class="empty-state">暂无桶，点击右上角「创建桶」开始使用</div>

      <!-- 创建弹层 -->
      <div v-if="createModal.show" class="modal" @click.self="createModal.show=false">
        <div class="modal-content">
          <h3>创建桶</h3>
          <div class="form-group">
            <label>桶名称（必填）</label>
            <input v-model="createModal.name" type="text" maxlength="63" placeholder="1-63 字符，用户内唯一" @keyup.enter="doCreate">
          </div>
          <div class="form-group">
            <label>描述（可选）</label>
            <textarea v-model="createModal.description" class="p8-bk-textarea" maxlength="255" placeholder="桶用途说明"></textarea>
          </div>
          <div class="form-group">
            <label>访问权限</label>
            <select v-model="createModal.visibility" class="p8-bk-select">
              <option value="private">私有（private）</option>
              <option value="internal">内部（internal）</option>
              <option value="public">公开（public）</option>
            </select>
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" @click="createModal.show=false">取消</button>
            <button class="btn btn-primary" style="width:auto" :disabled="createModal.busy" @click="doCreate">{{ createModal.busy ? '创建中…' : '创建' }}</button>
          </div>
        </div>
      </div>

      <!-- 编辑弹层：仅传修改项 -->
      <div v-if="editModal.show" class="modal" @click.self="editModal.show=false">
        <div class="modal-content">
          <h3>编辑桶「{{ editModal.name }}」</h3>
          <div class="form-group">
            <label>描述</label>
            <textarea v-model="editModal.description" class="p8-bk-textarea" maxlength="255" placeholder="桶用途说明"></textarea>
          </div>
          <div class="form-group">
            <label>容量上限（GB）</label>
            <input v-model="editModal.sizeLimitGb" type="number" min="0.001" step="any" placeholder="如 1 表示 1GB">
            <div class="p8-bk-hint">当前：{{ formatSize(Number(editModal.orig && editModal.orig.sizeLimit)) }}（1 GB = 1073741824 字节）</div>
          </div>
          <div class="form-group">
            <label>访问权限</label>
            <select v-model="editModal.visibility" class="p8-bk-select">
              <option value="private">私有（private）</option>
              <option value="internal">内部（internal）</option>
              <option value="public">公开（public）</option>
            </select>
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" @click="editModal.show=false">取消</button>
            <button class="btn btn-primary" style="width:auto" :disabled="editModal.busy" @click="doUpdate">{{ editModal.busy ? '保存中…' : '保存' }}</button>
          </div>
        </div>
      </div>
    </div>
  `,
};
