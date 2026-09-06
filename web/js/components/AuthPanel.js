// P8 Task 4：认证组件 AuthPanel —— 登录三方式 / 注册三方式 / 重置密码弹层
// 依赖 Task 3 契约：api.js 的 6 个用户接口、store.js、common.js 的 toast
// Vue 为全量版全局变量（index.html 先以非 module 方式加载），模块内直接解构
const { ref, reactive, onUnmounted } = Vue;
import {
  register, loginByAccount, loginByPhoneSms, loginByEmailSms,
  sendVerificationCode, resetPassword,
} from '../api.js';
import { store } from '../store.js';
import { toast } from '../common.js';

// 本地校验正则（与后端 proto 校验规则保持一致）
const PHONE_RE = /^1[3-9]\d{9}$/;
const EMAIL_RE = /\S+@\S+\.\S+/;

// 无构建链：直接注入全局 <style>，类名加 p8- 前缀防冲突
if (!document.getElementById('p8-auth-style')) {
  const s = document.createElement('style');
  s.id = 'p8-auth-style';
  s.textContent = `
    .p8-segment { display: flex; gap: 8px; margin-bottom: 20px; }
    .p8-segment button { flex: 1; padding: 8px 4px; border: 1px solid #ddd; background: #f7f7f7;
      color: #555; border-radius: 8px; cursor: pointer; font-size: 14px; transition: all .2s; }
    .p8-segment button.active { background: #667eea; border-color: #667eea; color: #fff; }
    .p8-code-btn { min-width: 110px; white-space: nowrap; padding: 12px 10px; font-size: 14px; }
    .p8-code-btn:disabled { opacity: .6; cursor: not-allowed; transform: none; box-shadow: none; }
    .p8-btn-loading:disabled { opacity: .7; cursor: not-allowed; transform: none; box-shadow: none; }
    .p8-field-hint { font-size: 12px; color: #999; margin-top: 4px; }
  `;
  document.head.appendChild(s);
}

export const AuthPanel = {
  name: 'AuthPanel',
  setup() {
    // ===== 主 Tab：login | register（忘记密码走弹层，不占 Tab）=====
    const mainTab = ref('login');

    // ===== 登录 =====
    const loginMode = ref('account'); // account | phone | email
    const login = reactive({ account: '', password: '', phone: '', phoneCode: '', email: '', emailCode: '' });
    const loginLoading = ref(false);

    // ===== 注册 =====
    const regMode = ref('name'); // name | phone | email
    const reg = reactive({ name: '', contact: '', password: '', code: '' });
    const regLoading = ref(false);

    // ===== 重置密码弹层 =====
    const showReset = ref(false);
    const reset = reactive({ contact: '', code: '', newPassword: '' });
    const resetLoading = ref(false);

    // ===== 验证码倒计时（登录/注册共用主倒计时，弹层独立一份）=====
    const countdown = ref(0);
    const modalCountdown = ref(0);
    const timers = [];
    const sendLoading = ref(false);
    const modalSendLoading = ref(false);

    onUnmounted(() => timers.forEach(clearInterval));

    function startCountdown(r) {
      r.value = 60;
      const t = setInterval(() => { if (--r.value <= 0) clearInterval(t); }, 1000);
      timers.push(t);
    }

    // 判断联系方式：手机号→{phone}，否则→{email}
    function contactField(v) {
      if (PHONE_RE.test(v)) return { phone: v };
      if (EMAIL_RE.test(v)) return { email: v };
      return null;
    }

    // ===== 发送验证码 =====
    // scene: 'login-phone' | 'login-email' | 'register' | 'reset'
    async function sendCode(scene) {
      let body, isModal = false;
      if (scene === 'login-phone') {
        if (!PHONE_RE.test(login.phone)) return toast('请输入正确的手机号', 'error');
        body = { phone: login.phone, type: 1 }; // 登录验证码 type=1
      } else if (scene === 'login-email') {
        if (!EMAIL_RE.test(login.email)) return toast('请输入正确的邮箱地址', 'error');
        body = { email: login.email, type: 1 };
      } else if (scene === 'register') {
        const ok = regMode.value === 'phone' ? PHONE_RE.test(reg.contact) : EMAIL_RE.test(reg.contact);
        if (!ok) return toast(regMode.value === 'phone' ? '请输入正确的手机号' : '请输入正确的邮箱地址', 'error');
        body = { ...contactField(reg.contact), type: 2 }; // 注册验证码 type=2
      } else {
        const f = contactField(reset.contact);
        if (!f) return toast('请输入正确的手机号或邮箱', 'error');
        body = { ...f, type: 3 }; // 重置密码验证码 type=3
        isModal = true;
      }
      try {
        if (isModal) modalSendLoading.value = true; else sendLoading.value = true;
        await sendVerificationCode(body);
        toast('验证码已发送，请查收', 'success');
        startCountdown(isModal ? modalCountdown : countdown);
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        modalSendLoading.value = false; sendLoading.value = false;
      }
    }

    // ===== 登录提交 =====
    async function doLogin() {
      try {
        let data;
        if (loginMode.value === 'account') {
          if (!login.account.trim()) return toast('请输入账号（用户名/手机号/邮箱）', 'error');
          if (login.password.length < 6 || login.password.length > 20) return toast('密码长度为 6-20 位', 'error');
          loginLoading.value = true;
          data = await loginByAccount({ account: login.account.trim(), password: login.password });
        } else if (loginMode.value === 'phone') {
          if (!PHONE_RE.test(login.phone)) return toast('请输入正确的手机号', 'error');
          if (!/^\d{6}$/.test(login.phoneCode)) return toast('请输入 6 位验证码', 'error');
          loginLoading.value = true;
          data = await loginByPhoneSms({ phone: login.phone, smsCode: login.phoneCode });
        } else {
          if (!EMAIL_RE.test(login.email)) return toast('请输入正确的邮箱地址', 'error');
          if (!/^\d{6}$/.test(login.emailCode)) return toast('请输入 6 位验证码', 'error');
          loginLoading.value = true;
          data = await loginByEmailSms({ email: login.email, smsCode: login.emailCode });
        }
        store.saveAuth(data.token, data.user);
        toast('登录成功', 'success');
        store.navigate('#/app');
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        loginLoading.value = false;
      }
    }

    // ===== 注册提交 =====
    async function doRegister() {
      try {
        let data;
        if (regMode.value === 'name') {
          if (!reg.name.trim()) return toast('请输入用户名', 'error');
          if (reg.name.trim().length > 20) return toast('用户名最长 20 个字符', 'error');
          if (reg.password.length < 6 || reg.password.length > 20) return toast('密码长度为 6-20 位', 'error');
          regLoading.value = true;
          data = await register({ name: reg.name.trim(), password: reg.password });
        } else {
          const f = contactField(reg.contact);
          const ok = regMode.value === 'phone' ? PHONE_RE.test(reg.contact) : EMAIL_RE.test(reg.contact);
          if (!f || !ok) return toast(regMode.value === 'phone' ? '请输入正确的手机号' : '请输入正确的邮箱地址', 'error');
          if (reg.password.length < 6 || reg.password.length > 20) return toast('密码长度为 6-20 位', 'error');
          if (!/^\d{6}$/.test(reg.code)) return toast('请输入 6 位验证码', 'error');
          regLoading.value = true;
          // 手机/邮箱注册不传 name，后端做 nickname 兜底
          data = await register({ ...f, password: reg.password, smsCode: reg.code });
        }
        store.saveAuth(data.token, data.user);
        toast('注册成功', 'success');
        store.navigate('#/app');
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        regLoading.value = false;
      }
    }

    // ===== 重置密码提交 =====
    async function doReset() {
      const f = contactField(reset.contact);
      if (!f) return toast('请输入正确的手机号或邮箱', 'error');
      if (!/^\d{6}$/.test(reset.code)) return toast('请输入 6 位验证码', 'error');
      if (reset.newPassword.length < 6 || reset.newPassword.length > 20) return toast('新密码长度为 6-20 位', 'error');
      try {
        resetLoading.value = true;
        await resetPassword({ ...f, verificationCode: reset.code, newPassword: reset.newPassword });
        toast('密码重置成功，请使用新密码登录', 'success');
        showReset.value = false;
        // 重置成功后预填账号，方便直接登录
        if (loginMode.value === 'account' && !login.account) login.account = reset.contact;
      } catch (e) {
        toast(e.message, 'error');
      } finally {
        resetLoading.value = false;
      }
    }

    function openReset() {
      reset.contact = ''; reset.code = ''; reset.newPassword = '';
      showReset.value = true;
    }

    return {
      mainTab, loginMode, login, loginLoading,
      regMode, reg, regLoading,
      showReset, reset, resetLoading, openReset, doReset,
      countdown, modalCountdown, sendLoading, modalSendLoading, sendCode,
      doLogin, doRegister,
    };
  },
  template: `
    <div class="auth-container">
      <div class="auth-header">
        <h1>云存储</h1>
        <p>安全便捷的个人云盘</p>
      </div>

      <div class="tabs">
        <button class="tab" :class="{active: mainTab==='login'}" @click="mainTab='login'">登录</button>
        <button class="tab" :class="{active: mainTab==='register'}" @click="mainTab='register'">注册</button>
      </div>

      <div class="form-panel">
        <!-- ==================== 登录 ==================== -->
        <template v-if="mainTab==='login'">
          <div class="p8-segment">
            <button :class="{active: loginMode==='account'}" @click="loginMode='account'">账号密码</button>
            <button :class="{active: loginMode==='phone'}" @click="loginMode='phone'">手机验证码</button>
            <button :class="{active: loginMode==='email'}" @click="loginMode='email'">邮箱验证码</button>
          </div>

          <template v-if="loginMode==='account'">
            <div class="form-group">
              <label>账号</label>
              <input v-model="login.account" type="text" placeholder="用户名 / 手机号 / 邮箱" @keyup.enter="doLogin">
            </div>
            <div class="form-group">
              <label>密码</label>
              <input v-model="login.password" type="password" placeholder="请输入密码" @keyup.enter="doLogin">
            </div>
          </template>

          <template v-else-if="loginMode==='phone'">
            <div class="form-group">
              <label>手机号</label>
              <input v-model="login.phone" type="tel" maxlength="11" placeholder="请输入手机号">
            </div>
            <div class="form-group">
              <label>验证码</label>
              <div class="code-input">
                <input v-model="login.phoneCode" type="text" maxlength="6" placeholder="6 位验证码" @keyup.enter="doLogin">
                <button class="btn btn-secondary p8-code-btn" :disabled="countdown>0 || sendLoading"
                  @click="sendCode('login-phone')">
                  {{ sendLoading ? '发送中…' : (countdown>0 ? countdown+'s 后重发' : '获取验证码') }}
                </button>
              </div>
            </div>
          </template>

          <template v-else>
            <div class="form-group">
              <label>邮箱</label>
              <input v-model="login.email" type="email" placeholder="请输入邮箱">
            </div>
            <div class="form-group">
              <label>验证码</label>
              <div class="code-input">
                <input v-model="login.emailCode" type="text" maxlength="6" placeholder="6 位验证码" @keyup.enter="doLogin">
                <button class="btn btn-secondary p8-code-btn" :disabled="countdown>0 || sendLoading"
                  @click="sendCode('login-email')">
                  {{ sendLoading ? '发送中…' : (countdown>0 ? countdown+'s 后重发' : '获取验证码') }}
                </button>
              </div>
            </div>
          </template>

          <button class="btn btn-primary p8-btn-loading" :disabled="loginLoading" @click="doLogin">
            {{ loginLoading ? '处理中…' : '登 录' }}
          </button>
          <div class="form-links">
            <a href="javascript:;" @click="openReset">忘记密码？</a>
          </div>
        </template>

        <!-- ==================== 注册 ==================== -->
        <template v-else>
          <div class="form-group">
            <label>注册方式</label>
            <div class="p8-segment" style="margin-bottom:0">
              <button :class="{active: regMode==='name'}" @click="regMode='name'">用户名</button>
              <button :class="{active: regMode==='phone'}" @click="regMode='phone'">手机号</button>
              <button :class="{active: regMode==='email'}" @click="regMode='email'">邮箱</button>
            </div>
          </div>

          <template v-if="regMode==='name'">
            <div class="form-group">
              <label>用户名</label>
              <input v-model="reg.name" type="text" maxlength="20" placeholder="1-20 个字符">
            </div>
            <div class="form-group">
              <label>密码</label>
              <input v-model="reg.password" type="password" placeholder="6-20 位密码" @keyup.enter="doRegister">
            </div>
          </template>

          <template v-else>
            <div class="form-group">
              <label>{{ regMode==='phone' ? '手机号' : '邮箱' }}</label>
              <input v-model="reg.contact" :type="regMode==='phone' ? 'tel' : 'email'"
                :maxlength="regMode==='phone' ? 11 : 50" :placeholder="regMode==='phone' ? '请输入手机号' : '请输入邮箱'">
            </div>
            <div class="form-group">
              <label>验证码</label>
              <div class="code-input">
                <input v-model="reg.code" type="text" maxlength="6" placeholder="6 位验证码（必填）" @keyup.enter="doRegister">
                <button class="btn btn-secondary p8-code-btn" :disabled="countdown>0 || sendLoading"
                  @click="sendCode('register')">
                  {{ sendLoading ? '发送中…' : (countdown>0 ? countdown+'s 后重发' : '获取验证码') }}
                </button>
              </div>
              <div class="p8-field-hint">注册必须使用验证码验证{{ regMode==='phone' ? '手机' : '邮箱' }}</div>
            </div>
            <div class="form-group">
              <label>密码</label>
              <input v-model="reg.password" type="password" placeholder="6-20 位密码" @keyup.enter="doRegister">
            </div>
          </template>

          <button class="btn btn-primary p8-btn-loading" :disabled="regLoading" @click="doRegister">
            {{ regLoading ? '处理中…' : '注 册' }}
          </button>
        </template>
      </div>

      <!-- ==================== 重置密码弹层 ==================== -->
      <div v-if="showReset" class="modal" @click.self="showReset=false">
        <div class="modal-content">
          <h3>重置密码</h3>
          <div class="form-group">
            <label>手机号 / 邮箱</label>
            <input v-model="reset.contact" type="text" placeholder="注册时绑定的手机号或邮箱">
          </div>
          <div class="form-group">
            <label>验证码</label>
            <div class="code-input">
              <input v-model="reset.code" type="text" maxlength="6" placeholder="6 位验证码">
              <button class="btn btn-secondary p8-code-btn" :disabled="modalCountdown>0 || modalSendLoading"
                @click="sendCode('reset')">
                {{ modalSendLoading ? '发送中…' : (modalCountdown>0 ? modalCountdown+'s 后重发' : '获取验证码') }}
              </button>
            </div>
          </div>
          <div class="form-group">
            <label>新密码</label>
            <input v-model="reset.newPassword" type="password" placeholder="6-20 位新密码" @keyup.enter="doReset">
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" @click="showReset=false">取消</button>
            <button class="btn btn-primary p8-btn-loading" :disabled="resetLoading" @click="doReset">
              {{ resetLoading ? '处理中…' : '确认重置' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  `,
};
