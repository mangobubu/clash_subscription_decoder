<template>
  <div class="login-container">
    <div class="login-box">
      <div class="login-header">
        <h2 class="title">{{ isInitMode ? '首次运行配置' : 'Clash Proxy Decoder' }}</h2>
        <p class="subtitle">{{ isInitMode ? '检测到系统尚未初始化，请创建超级管理员' : '安全控制台' }}</p>
      </div>
      
      <el-form class="login-form" @submit.prevent="handleLogin">
        <el-form-item>
          <el-input 
            v-model="username" 
            placeholder="请输入管理员账号" 
            prefix-icon="User" 
            size="large"
          />
        </el-form-item>

        <el-form-item>
          <el-input 
            v-model="password" 
            type="password" 
            placeholder="请输入管理员密码" 
            prefix-icon="Lock" 
            size="large"
            show-password
          />
        </el-form-item>

        <el-form-item v-if="captchaEnabled">
          <div class="captcha-wrapper">
            <el-input 
              v-model="captchaValue" 
              placeholder="请输入验证码" 
              prefix-icon="Key" 
              size="large"
              class="captcha-input"
            />
            <div class="captcha-img-box" @click="fetchCaptcha" title="点击刷新验证码">
              <img v-if="captchaImage" :src="captchaImage" alt="验证码" />
              <el-icon v-else class="is-loading"><Loading /></el-icon>
            </div>
          </div>
        </el-form-item>

        <el-button 
          type="primary" 
          native-type="submit" 
          class="submit-btn" 
          size="large" 
          :loading="isLoading"
        >
          {{ isInitMode ? '创建账号' : '安全登录' }}
        </el-button>
      </el-form>
    </div>
    
    <!-- Background animation elements -->
    <div class="bg-shape shape1"></div>
    <div class="bg-shape shape2"></div>
    <div class="bg-shape shape3"></div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { ElMessage } from 'element-plus';
import { Loading } from '@element-plus/icons-vue';
import axios from 'axios';
import { buildBackendUrl } from './config/backend';

const emit = defineEmits(['login-success']);

const username = ref('');
const password = ref('');
const captchaValue = ref('');
const captchaId = ref('');
const captchaImage = ref('');
const captchaEnabled = ref(false);
const isLoading = ref(false);
const isInitMode = ref(false);

const fetchCaptcha = async () => {
  try {
    const initRes = await axios.get(buildBackendUrl('/api/check-init'));
    isInitMode.value = initRes.data.data.need_init;
  } catch (error) {
    console.error('Failed to check init status', error);
  }

  try {
    const res = await axios.get(buildBackendUrl('/api/captcha'));
    if (res.data.code === 200) {
      if (res.data.data.enabled) {
        captchaEnabled.value = true;
        captchaId.value = res.data.data.captcha_id;
        captchaImage.value = res.data.data.b64s;
        captchaValue.value = '';
      } else {
        captchaEnabled.value = false;
      }
    }
  } catch (error) {
    console.error('获取验证码失败', error);
  }
};

const handleLogin = async () => {
  if (!username.value) {
    ElMessage.warning('账号不能为空');
    return;
  }
  if (!password.value) {
    ElMessage.warning('密码不能为空');
    return;
  }
  if (captchaEnabled.value && !captchaValue.value) {
    ElMessage.warning('验证码不能为空');
    return;
  }

  if (isInitMode.value) {
    isLoading.value = true;
    try {
      await axios.post(buildBackendUrl('/api/init'), {
        username: username.value,
        password: password.value,
        captcha_id: captchaId.value,
        captcha_value: captchaValue.value
      });
      ElMessage.success('超级管理员创建成功！请重新登录');
      isInitMode.value = false;
      password.value = '';
      captchaValue.value = '';
      await fetchCaptcha();
    } catch (error: any) {
      ElMessage.error(error.response?.data?.message || '初始化失败，请重试');
      await fetchCaptcha();
    } finally {
      isLoading.value = false;
    }
    return;
  }

  isLoading.value = true;
  try {
    const res = await axios.post(buildBackendUrl('/api/login'), {
      username: username.value,
      password: password.value,
      captcha_id: captchaId.value,
      captcha_value: captchaValue.value
    });

    if (res.data.code === 200) {
      ElMessage.success('登录成功');
      localStorage.setItem('token', res.data.token);
      emit('login-success');
    } else {
      ElMessage.error(res.data.message || '登录失败');
      if (captchaEnabled.value) {
        fetchCaptcha();
      }
    }
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || '登录异常，请检查网络');
    if (captchaEnabled.value) {
      fetchCaptcha();
    }
  } finally {
    isLoading.value = false;
  }
};

onMounted(() => {
  fetchCaptcha();
});
</script>

<style scoped>
.login-container {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background:
    radial-gradient(circle at 12% 10%, rgba(14, 165, 233, 0.18), transparent 34%),
    radial-gradient(circle at 88% 90%, rgba(16, 185, 129, 0.12), transparent 30%),
    #07111f;
  position: relative;
  overflow: hidden;
  font-family: 'Inter', system-ui, sans-serif;
  box-sizing: border-box;
}

/* 动态流光背景元素 */
.bg-shape {
  position: absolute;
  filter: blur(90px);
  z-index: 0;
  animation: float 22s infinite alternate ease-in-out;
  opacity: 0.42;
}

.shape1 {
  width: 420px;
  height: 420px;
  background: rgba(14, 165, 233, 0.34);
  top: -100px;
  left: -100px;
}

.shape2 {
  width: 360px;
  height: 360px;
  background: rgba(20, 184, 166, 0.28);
  bottom: -50px;
  right: -50px;
  animation-delay: -5s;
}

.shape3 {
  width: 260px;
  height: 260px;
  background: rgba(148, 163, 184, 0.18);
  top: 40%;
  left: 50%;
  animation-delay: -10s;
}

@keyframes float {
  0% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(50px, 30px) scale(1.1); }
  100% { transform: translate(-30px, 50px) scale(0.9); }
}

.login-box {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 430px;
  padding: 34px;
  background: rgba(12, 24, 38, 0.84);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 18px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.42);
  animation: slideUpFade 0.6s cubic-bezier(0.16, 1, 0.3, 1) forwards;
  opacity: 0;
  transform: translateY(20px);
}

@keyframes slideUpFade {
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.login-header {
  text-align: center;
  margin-bottom: 28px;
}

.login-header h2 {
  margin: 0;
  font-size: 26px;
  font-weight: 700;
  color: #f8fafc;
  letter-spacing: 0;
  background: linear-gradient(135deg, #e0f2fe 0%, #7dd3fc 52%, #a7f3d0 100%);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
}

.login-header p {
  margin: 8px 0 0;
  font-size: 14px;
  color: #94a3b8;
  letter-spacing: 0;
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

:deep(.el-input__wrapper) {
  background-color: rgba(7, 17, 31, 0.76) !important;
  box-shadow: 0 0 0 1px rgba(148, 163, 184, 0.18) inset !important;
  border-radius: 10px;
  transition: all 0.3s ease;
}

:deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 1px #38bdf8 inset, 0 0 0 4px rgba(56, 189, 248, 0.1) !important;
  background-color: rgba(7, 17, 31, 0.9) !important;
}

:deep(.el-input__inner) {
  color: #f8fafc;
  height: 48px;
}

.captcha-wrapper {
  display: flex;
  gap: 12px;
  align-items: center;
  width: 100%;
  min-width: 0;
}

.captcha-input {
  flex: 1;
  min-width: 0;
}

.captcha-img-box {
  width: 132px;
  height: 48px;
  flex: 0 0 132px;
  border-radius: 10px;
  overflow: hidden;
  background: rgba(7, 17, 31, 0.76);
  border: 1px solid rgba(148, 163, 184, 0.18);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.2s, box-shadow 0.2s;
}

.captcha-img-box:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
}

.captcha-img-box img {
  width: 100%;
  height: 100%;
  object-fit: fill;
}

.submit-btn {
  width: 100%;
  height: 48px;
  border-radius: 10px;
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0;
  background: linear-gradient(135deg, #0284c7 0%, #0ea5e9 55%, #14b8a6 100%);
  border: none;
  margin-top: 10px;
  transition: all 0.3s ease;
  box-shadow: 0 12px 26px rgba(14, 165, 233, 0.24);
}

.submit-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 16px 34px rgba(14, 165, 233, 0.34);
  opacity: 0.95;
}

.submit-btn:active {
  transform: translateY(0);
}

@media (max-width: 640px) {
  .login-container {
    align-items: stretch;
    padding: 14px;
  }

  .login-box {
    align-self: center;
    max-width: 100%;
    padding: 24px 18px;
    border-radius: 14px;
  }

  .login-header h2 {
    font-size: 22px;
  }

  .captcha-wrapper {
    flex-direction: column;
    align-items: stretch;
  }

  .captcha-img-box {
    width: 100%;
    flex-basis: 48px;
  }

  .bg-shape {
    opacity: 0.28;
  }
}
</style>
