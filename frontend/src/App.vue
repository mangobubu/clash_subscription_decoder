<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { ElLoading, ElMessage, ElMessageBox } from "element-plus";
import {
  Link,
  Edit,
  Delete,
  Loading,
  ArrowDown,
  Search,
  Plus,
  MoreFilled,
  Select,
  Refresh,
  CircleClose,
  Connection,
  CopyDocument,
  Download,
  Upload,
  Lock,
  Setting,
  Promotion,
  Remove,
  SwitchButton,
  InfoFilled,
} from "@element-plus/icons-vue";
import axios from "axios";
import { Codemirror } from "vue-codemirror";
import { yaml as codemirrorYaml } from "@codemirror/lang-yaml";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import jsYaml from "js-yaml";
import Login from "./Login.vue";
import IconTooltipButton from "./components/IconTooltipButton.vue";
import { buildBackendUrl, buildPublicBackendUrl } from "./config/backend";

const isLoggedIn = ref(false);
const isVerifying = ref(true);
const isInitialDataLoading = ref(false);

const isAppBootstrapping = computed(() => isVerifying.value || isInitialDataLoading.value);
const appBootstrapTitle = computed(() =>
  isVerifying.value ? "正在验证登录状态" : "正在加载数据库数据",
);
const appBootstrapDesc = computed(() =>
  isVerifying.value
    ? "正在确认当前会话，请稍候。"
    : "正在读取订阅、自定义节点、策略组和分流规则，加载完成前暂不可操作。",
);

const onLoginSuccess = async () => {
  isInitialDataLoading.value = true;
  isLoggedIn.value = true;
  await loadInitialAppData();
};

// CodeMirror 配置
const cmExtensions = [
  codemirrorYaml(),
  oneDark,
  EditorState.readOnly.of(true),
  EditorView.lineWrapping,
];

// 状态定义
const inputUrl = ref("");
const isLoading = ref(false);
const activeTab = ref("nodes");
const errorMsg = ref("");
const hasSubscription = ref(false);
const result = ref<{ url: string; raw_response: string; decoded: string } | null>(
  null,
);

interface SubscriptionProfile {
  id: number;
  name: string;
  source_type: "remote" | "local";
  url: string;
  local_content: string;
  sources?: SubscriptionSource[];
  subscription_count?: number;
  has_token: boolean;
  created_at: number;
  updated_at: number;
}

interface SubscriptionSource {
  id?: number;
  url: string;
  is_primary: boolean;
  sort_order?: number;
}

interface ProfileSourceForm {
  url: string;
  is_primary: boolean;
}

type ProviderKind = "proxy" | "rule";

interface ProviderDisplayItem {
  kind: ProviderKind;
  name: string;
  type: string;
  url: string;
  path: string;
  behavior: string;
  format: string;
  interval: string;
  usageCount: number;
  config: Record<string, any>;
}

interface ParseDiagnostic {
  type: "success" | "info" | "warning" | "error";
  title: string;
  description: string;
}

const profiles = ref<SubscriptionProfile[]>([]);
const activeProfileId = ref<number | null>(null);
const isProfilesLoading = ref(false);
const profileDialogVisible = ref(false);
const isSubmittingProfile = ref(false);
const editingProfileId = ref<number | null>(null);
const profileForm = ref({
  name: "",
  source_type: "remote" as "remote" | "local",
  url: "",
  sources: [{ url: "", is_primary: true }] as ProfileSourceForm[],
});

const currentProfile = computed(() =>
  profiles.value.find((profile) => profile.id === activeProfileId.value) || null,
);

const createEmptyProfileSource = (isPrimary = false): ProfileSourceForm => ({
  url: "",
  is_primary: isPrimary,
});

const normalizeProfileSourcesForForm = (
  sources: SubscriptionSource[] | undefined,
  fallbackUrl = "",
): ProfileSourceForm[] => {
  const rawSources =
    sources && sources.length > 0
      ? sources
      : fallbackUrl
        ? [{ url: fallbackUrl, is_primary: true }]
        : [{ url: "", is_primary: true }];
  const hasPrimary = rawSources.some((source) => source.is_primary);
  const normalized = rawSources.map((source, index) => ({
    url: source.url || "",
    is_primary: Boolean(source.is_primary) || (!hasPrimary && index === 0),
  }));
  const primaryIndex = normalized.findIndex((source) => source.is_primary);
  normalized.forEach((source, index) => {
    source.is_primary = index === (primaryIndex >= 0 ? primaryIndex : 0);
  });
  return normalized;
};

const primaryProfileUrl = (profile: SubscriptionProfile | null) => {
  if (!profile) return "";
  return profile.sources?.find((source) => source.is_primary)?.url || profile.url || "";
};

const profileDisplaySources = (profile: SubscriptionProfile | null) => {
  if (!profile || profile.source_type !== "remote") return [];
  const rawSources =
    profile.sources && profile.sources.length > 0
      ? profile.sources
      : profile.url
        ? [{ url: profile.url, is_primary: true, sort_order: 0 }]
        : [];
  return [...rawSources].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
};

const profileSubscriptionCount = (profile: SubscriptionProfile | null) => {
  if (!profile || profile.source_type !== "remote") return 0;
  return Math.max(profile.subscription_count || 0, profile.sources?.length || 0, profile.url ? 1 : 0);
};

const isMultiSubscriptionProfile = computed(() => profileSubscriptionCount(currentProfile.value) > 1);

const currentProfileName = computed(() => currentProfile.value?.name || "当前配置");
const profileScopedUrl = (path: string) => {
  if (!activeProfileId.value) return buildBackendUrl(path);
  const separator = path.includes("?") ? "&" : "?";
  return buildBackendUrl(`${path}${separator}profile_id=${activeProfileId.value}`);
};

// 最终订阅生成状态
const isCopyingSubLink = ref(false);
const isGeneratingSubLink = ref(false);
const subLinkDialogVisible = ref(false);
const subLinkDialogTitle = ref("复制订阅地址");
const showRegeneratedWarning = ref(false);
const finalSubLink = ref("");
const surgeLatestSubLink = ref("");
const surge576SubLink = ref("");
const shadowrocketSubLink = ref("");
const shadowrocketInstallLink = ref("");

const buildSubLink = (token: string) => buildPublicBackendUrl(`/sub?token=${encodeURIComponent(token)}`);
const buildSurgeLatestSubLink = (token: string) =>
  buildPublicBackendUrl(`/surge.conf?token=${encodeURIComponent(token)}`);
const buildSurge576SubLink = (token: string) =>
  buildPublicBackendUrl(`/surge-5.7.6.conf?token=${encodeURIComponent(token)}`);
const buildShadowrocketSubLink = (token: string) =>
  buildPublicBackendUrl(`/shadowrocket.conf?token=${encodeURIComponent(token)}`);
const buildShadowrocketInstallLink = (token: string) =>
  buildPublicBackendUrl(`/shadowrocket/install?token=${encodeURIComponent(token)}`);

const setFinalSubLinks = (token: string) => {
  finalSubLink.value = buildSubLink(token);
  surgeLatestSubLink.value = buildSurgeLatestSubLink(token);
  surge576SubLink.value = buildSurge576SubLink(token);
  shadowrocketSubLink.value = buildShadowrocketSubLink(token);
  shadowrocketInstallLink.value = buildShadowrocketInstallLink(token);
};

const copyTextToClipboard = async (text: string, successMessage = "链接已复制到剪贴板！") => {
  try {
    await navigator.clipboard.writeText(text);
    ElMessage.success(successMessage);
  } catch (err) {
    ElMessage.error("复制失败，请手动复制");
  }
};

const copySubLink = () => copyTextToClipboard(finalSubLink.value);

const copySurgeLatestSubLink = () =>
  copyTextToClipboard(surgeLatestSubLink.value, "Surge 最新版配置地址已复制到剪贴板！");

const copySurge576SubLink = () =>
  copyTextToClipboard(surge576SubLink.value, "Surge 5.7.6 兼容配置地址已复制到剪贴板！");

const copyShadowrocketSubLink = () =>
  copyTextToClipboard(shadowrocketSubLink.value, "Shadowrocket 配置地址已复制到剪贴板！");

const copyShadowrocketInstallLink = () =>
  copyTextToClipboard(shadowrocketInstallLink.value, "Shadowrocket 安装链接已复制到剪贴板！");

const installShadowrocketConfig = () => {
  if (!shadowrocketInstallLink.value) {
    ElMessage.warning("请先生成或获取订阅地址");
    return;
  }
  window.location.href = shadowrocketInstallLink.value;
};

const copyCurrentSubLink = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  isCopyingSubLink.value = true;
  try {
    const res = await axios.get(buildBackendUrl(`/api/profiles/${activeProfileId.value}/sub-token`));
    if (res.data.code !== 200) {
      ElMessage.error(res.data.message || "获取订阅地址失败");
      return;
    }

    const data = res.data.data || {};
    if (!data.has_token || !data.token) {
      ElMessage.warning("当前还没有订阅地址，请先重新生成订阅");
      return;
    }

    setFinalSubLinks(data.token);
    subLinkDialogTitle.value = `复制订阅地址 - ${currentProfileName.value}`;
    showRegeneratedWarning.value = false;
    subLinkDialogVisible.value = true;
  } catch (err: any) {
    console.error(err);
    ElMessage.error(err.response?.data?.message || "获取订阅地址失败，请检查网络或登录状态");
  } finally {
    isCopyingSubLink.value = false;
  }
};

const regenerateSubLink = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  try {
    await ElMessageBox.confirm(
      `重新生成「${currentProfileName.value}」的订阅会覆盖该配置旧 token，旧订阅地址将立即失效。是否继续？`,
      "重新生成订阅确认",
      {
        confirmButtonText: "确认重新生成",
        cancelButtonText: "取消",
        type: "warning",
        customClass: "glass-dialog",
      },
    );
  } catch {
    return;
  }

  isGeneratingSubLink.value = true;
  try {
    const res = await axios.post(buildBackendUrl(`/api/profiles/${activeProfileId.value}/generate-sub-token`));
    if (res.data.code === 200) {
      setFinalSubLinks(res.data.data.token);
      subLinkDialogTitle.value = `订阅地址已重新生成 - ${currentProfileName.value}`;
      showRegeneratedWarning.value = true;
      subLinkDialogVisible.value = true;
    } else {
      ElMessage.error(res.data.message || "重新生成失败");
    }
  } catch (err: any) {
    console.error(err);
    ElMessage.error(err.response?.data?.message || "重新生成失败，请检查网络或登录状态");
  } finally {
    isGeneratingSubLink.value = false;
  }
};

const loadProfiles = async (preferredProfileId?: number) => {
  isProfilesLoading.value = true;
  try {
    const res = await axios.get(buildBackendUrl("/api/profiles"));
    if (res.data.code === 200) {
      profiles.value = res.data.data || [];
      const savedProfileId = Number(localStorage.getItem("active_profile_id") || 0);
      const nextProfile =
        profiles.value.find((profile) => profile.id === preferredProfileId) ||
        profiles.value.find((profile) => profile.id === savedProfileId) ||
        profiles.value[0] ||
        null;
      activeProfileId.value = nextProfile?.id || null;
      if (activeProfileId.value) {
        localStorage.setItem("active_profile_id", String(activeProfileId.value));
        inputUrl.value = primaryProfileUrl(nextProfile);
      }
    }
  } catch (err: any) {
    console.error(err);
    ElMessage.error(err.response?.data?.message || "获取配置列表失败");
  } finally {
    isProfilesLoading.value = false;
  }
};

interface LoadSubscriptionOptions {
  preserveActiveTab?: boolean;
  preferredTab?: string;
}

function isResultTabAvailable(tab: string) {
  if (tab === "nodes") return parsedNodes.value.length > 0;
  if (tab === "groups") return proxyGroups.value.length > 0;
  if (tab === "rules") return parsedRuleLines.value.length > 0;
  if (tab === "providers") return providerItems.value.length > 0;
  if (tab === "diagnostics") return parseDiagnostics.value.length > 0;
  return tab === "text" || tab === "raw";
}

function applyActiveTabAfterSubscriptionLoad(options: LoadSubscriptionOptions, previousTab: string) {
  const shouldPreserveActiveTab = options.preserveActiveTab ?? true;
  const preferredTab = options.preferredTab || (shouldPreserveActiveTab ? previousTab : "");
  if (preferredTab && isResultTabAvailable(preferredTab)) {
    activeTab.value = preferredTab;
    return;
  }
  if (parsedNodes.value.length > 0) {
    activeTab.value = "nodes";
  } else if (proxyGroups.value.length > 0) {
    activeTab.value = "groups";
  } else if (parsedRuleLines.value.length > 0) {
    activeTab.value = "rules";
  } else if (providerItems.value.length > 0) {
    activeTab.value = "providers";
  } else if (parseDiagnostics.value.length > 0) {
    activeTab.value = "diagnostics";
  } else {
    activeTab.value = "text";
  }
}

const loadSubscription = async (options: LoadSubscriptionOptions = {}) => {
  if (!activeProfileId.value) {
    result.value = null;
    hasSubscription.value = false;
    return;
  }
  const previousTab = activeTab.value;
  try {
    const res = await axios.get(buildBackendUrl(`/api/profiles/${activeProfileId.value}/subscription`));
    if (res.data.code === 200 && res.data.data) {
      inputUrl.value = res.data.data.url;
      result.value = res.data.data;
      hasSubscription.value = true;
      applyActiveTabAfterSubscriptionLoad(options, previousTab);
    }
  } catch (e: any) {
    result.value = null;
    hasSubscription.value = false;
    inputUrl.value = primaryProfileUrl(currentProfile.value);
  }
};

const selectProfile = async (profile: SubscriptionProfile) => {
  if (activeProfileId.value === profile.id) return;
  activeProfileId.value = profile.id;
  localStorage.setItem("active_profile_id", String(profile.id));
  inputUrl.value = primaryProfileUrl(profile);
  errorMsg.value = "";
  dirtyRulesMap.value = {};
  await fetchCustomData();
  await loadSubscription({ preserveActiveTab: false });
};

const openCreateProfileDialog = () => {
  editingProfileId.value = null;
  profileForm.value = {
    name: "",
    source_type: "remote",
    url: "",
    sources: [createEmptyProfileSource(true)],
  };
  profileDialogVisible.value = true;
};

const openEditProfileDialog = (profile: SubscriptionProfile) => {
  editingProfileId.value = profile.id;
  profileForm.value = {
    name: profile.name,
    source_type: profile.source_type,
    url: primaryProfileUrl(profile),
    sources: normalizeProfileSourcesForForm(profile.sources, profile.url),
  };
  profileDialogVisible.value = true;
};

const addProfileSource = () => {
  profileForm.value.sources.push(createEmptyProfileSource(false));
};

const setPrimaryProfileSource = (index: number) => {
  profileForm.value.sources.forEach((source, sourceIndex) => {
    source.is_primary = sourceIndex === index;
  });
};

const removeProfileSource = (index: number) => {
  if (profileForm.value.sources.length <= 1) {
    ElMessage.warning("至少保留一个订阅地址");
    return;
  }
  const removedPrimary = profileForm.value.sources[index]?.is_primary;
  profileForm.value.sources.splice(index, 1);
  if (removedPrimary && profileForm.value.sources.length > 0) {
    setPrimaryProfileSource(0);
  }
};

const saveProfile = async () => {
  if (!profileForm.value.name.trim()) {
    ElMessage.warning("请输入配置名称");
    return;
  }
  const remoteSources = profileForm.value.sources.map((source) => ({
    url: source.url.trim(),
    is_primary: source.is_primary,
  }));
  if (profileForm.value.source_type === "remote") {
    if (remoteSources.some((source) => !source.url)) {
      ElMessage.warning("请补全所有远程订阅地址");
      return;
    }
    if (remoteSources.filter((source) => source.is_primary).length !== 1) {
      ElMessage.warning("请选择一个主订阅");
      return;
    }
  }
  isSubmittingProfile.value = true;
  try {
    const primarySource = remoteSources.find((source) => source.is_primary);
    const payload = {
      ...profileForm.value,
      url: profileForm.value.source_type === "remote" ? primarySource?.url || "" : "",
      sources: profileForm.value.source_type === "remote" ? remoteSources : [],
      local_content: "",
    };
    const res = editingProfileId.value
      ? await axios.put(buildBackendUrl(`/api/profiles/${editingProfileId.value}`), payload)
      : await axios.post(buildBackendUrl("/api/profiles"), payload);
    if (res.data.code === 200) {
      const profileId = res.data.data?.id || editingProfileId.value;
      ElMessage.success(editingProfileId.value ? "配置更新成功" : "配置创建成功");
      profileDialogVisible.value = false;
      await loadProfiles(profileId);
      await fetchCustomData();
      await loadSubscription();
    } else {
      ElMessage.error(res.data.message || "保存配置失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "保存配置失败");
  } finally {
    isSubmittingProfile.value = false;
  }
};

const deleteProfile = async (profile: SubscriptionProfile) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除配置「${profile.name}」吗？该配置下的自定义节点、策略组和规则也会删除。`,
      "删除配置确认",
      {
        confirmButtonText: "确认删除",
        cancelButtonText: "取消",
        type: "warning",
      },
    );
  } catch {
    return;
  }

  try {
    const res = await axios.delete(buildBackendUrl(`/api/profiles/${profile.id}`));
    if (res.data.code === 200) {
      ElMessage.success("配置已删除");
      await loadProfiles();
      await fetchCustomData();
      await loadSubscription();
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "删除配置失败");
  }
};

const refreshCurrentProfile = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  isLoading.value = true;
  errorMsg.value = "";
  const previousTab = activeTab.value;
  try {
    const res = await axios.post(buildBackendUrl(`/api/profiles/${activeProfileId.value}/refresh`));
    if (res.data.code === 200) {
      result.value = res.data.data;
      hasSubscription.value = true;
      inputUrl.value = res.data.data.url || "";
      await loadProfiles(activeProfileId.value);
      await fetchCustomData();
      ElMessage.success("当前配置已刷新");
      applyActiveTabAfterSubscriptionLoad({ preserveActiveTab: true }, previousTab);
    }
  } catch (err: any) {
    const msg = err.response?.data?.error || err.response?.data?.message || "刷新配置失败";
    errorMsg.value = msg;
    ElMessage.error("刷新配置失败");
  } finally {
    isLoading.value = false;
  }
};

// ---------------------- 自定义资源字典状态 ----------------------
const customNodesDict = ref<Record<string, any>>({});
const customGroupsDict = ref<Record<string, any>>({});
const customRulesDict = ref<Record<string, any>>({});

const fetchCustomData = async () => {
  if (!activeProfileId.value) {
    customNodesDict.value = {};
    customGroupsDict.value = {};
    customRulesDict.value = {};
    return;
  }
  try {
    const [nodesRes, groupsRes, rulesRes] = await Promise.all([
      axios.get(profileScopedUrl("/api/custom-nodes")),
      axios.get(profileScopedUrl("/api/custom-groups")),
      axios.get(profileScopedUrl("/api/custom-rules")),
    ]);
    const nDict: Record<string, any> = {};
    if (nodesRes.data.code === 200) {
      nodesRes.data.data.forEach((n: any) => (nDict[n.Name || n.name] = n));
    }
    customNodesDict.value = nDict;

    const gDict: Record<string, any> = {};
    if (groupsRes.data.code === 200) {
      groupsRes.data.data.forEach((g: any) => (gDict[g.Name || g.name] = g));
    }
    customGroupsDict.value = gDict;

    const rDict: Record<string, any> = {};
    if (rulesRes.data.code === 200) {
      rulesRes.data.data.forEach((r: any) => {
        let key = "";
        if (!r.Payload || r.Payload === "-") {
          key = r.Type;
        } else {
          key = `${r.Type},${r.Payload}`;
        }
        rDict[key] = r;
      });
    }
    customRulesDict.value = rDict;
  } catch (e) {
    console.error("获取自定义数据失败", e);
  }
};

const loadInitialAppData = async () => {
  isInitialDataLoading.value = true;
  try {
    await loadProfiles();
    await fetchCustomData();
    await loadSubscription();
  } finally {
    isInitialDataLoading.value = false;
  }
};

onMounted(async () => {
  window.addEventListener('auth-failed', () => {
    clearProxySettingsState();
    proxySettingsVisible.value = false;
    isLoggedIn.value = false;
    isInitialDataLoading.value = false;
  });

  const token = localStorage.getItem('token');
  if (token) {
    try {
      await axios.get(buildBackendUrl("/api/verify"));
      isLoggedIn.value = true;
      isVerifying.value = false;
      await loadInitialAppData();
    } catch {
      localStorage.removeItem('token');
    }
  }
  isVerifying.value = false;
});

// 节点接口定义
interface ProxyNode {
  name: string;
  type: string;
  server: string;
  port: string | number;
  details: Record<string, any>;
}

type SortResourceType = "nodes" | "groups";

type SubscriptionResourceType = SortResourceType;

interface DragSortState {
  resourceType: SortResourceType;
  fromIndex: number;
}

// 快速填入 Mock 地址
const handleQuickMock = () => {
  if (currentProfile.value?.source_type === "local") {
    ElMessage.warning("本地手动配置不需要订阅地址，请添加节点后刷新");
    return;
  }
  if (isMultiSubscriptionProfile.value) {
    ElMessage.warning("多订阅配置请在配置编辑中维护订阅地址");
    return;
  }
  inputUrl.value = "mock.clash.local/sub";
  handleDecode();
};

// 清除输入
const handleClear = () => {
  inputUrl.value = "";
  result.value = null;
  errorMsg.value = "";
};

// 解析并获取 Base64 内容
const handleDecode = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先创建或选择一个配置");
    return;
  }
  if (currentProfile.value?.source_type === "local") {
    await refreshCurrentProfile();
    return;
  }
  if (isMultiSubscriptionProfile.value) {
    await refreshCurrentProfile();
    return;
  }
  const url = inputUrl.value.trim();
  if (!url) {
    ElMessage.warning("请输入订阅或配置地址");
    return;
  }

  isLoading.value = true;
  errorMsg.value = "";
  result.value = null;

  try {
    const response = await axios.post(buildBackendUrl("/api/decode"), {
      url,
      profile_id: activeProfileId.value,
    });
    if (response.data && response.data.code === 200) {
      result.value = response.data.data;
      hasSubscription.value = true;
      await loadProfiles(activeProfileId.value);
      await fetchCustomData();
      ElMessage.success("成功拉取并完成 Base64 解码！");
      applyActiveTabAfterSubscriptionLoad({ preserveActiveTab: false }, activeTab.value);
    } else {
      throw new Error(response.data.message || "未知错误");
    }
  } catch (error: any) {
    console.error(error);
    let msg = "网络连接失败，请检查后端服务是否正常启动，且当前设备可以访问该地址";
    if (error.response && error.response.data) {
      msg = error.response.data.message || msg;
      if (error.response.data.error) {
        msg += ` (${error.response.data.error})`;
      }
    } else if (error.message) {
      msg = error.message;
    }
    errorMsg.value = msg;
    ElMessage.error("获取或解码失败");
  } finally {
    isLoading.value = false;
  }
};

// 核心响应式配置对象
const parsedConfig = computed<any>(() => {
  if (!result.value || !result.value.decoded) return null;
  try {
    return jsYaml.load(result.value.decoded);
  } catch (err) {
    console.error("YAML 解析失败:", err);
    return null;
  }
});

const isPlainObject = (value: unknown): value is Record<string, any> =>
  value !== null && typeof value === "object" && !Array.isArray(value);

const parsedConfigMap = computed<Record<string, any> | null>(() =>
  isPlainObject(parsedConfig.value) ? parsedConfig.value : null,
);

const topLevelArrayLength = (key: string) => {
  const value = parsedConfigMap.value?.[key];
  return Array.isArray(value) ? value.length : 0;
};

const parsedRuleLines = computed<string[]>(() => {
  const rules = parsedConfigMap.value?.rules;
  return Array.isArray(rules) ? rules.filter((rule) => typeof rule === "string") : [];
});

const providerValue = (value: unknown) => {
  if (Array.isArray(value)) return value.join(", ");
  if (typeof value === "boolean") return value ? "是" : "否";
  if (value === null || value === undefined || value === "") return "-";
  return String(value);
};

const proxyProviderUsage = computed<Record<string, number>>(() => {
  const usage: Record<string, number> = {};
  for (const group of proxyGroups.value) {
    const usedProviders = Array.isArray(group.use) ? group.use : [];
    usedProviders.forEach((name: string) => {
      usage[name] = (usage[name] || 0) + 1;
    });
  }
  return usage;
});

const ruleProviderUsage = computed<Record<string, number>>(() => {
  const usage: Record<string, number> = {};
  parsedRuleLines.value.forEach((rule) => {
    const parts = rule.split(",").map((part) => part.trim());
    if (parts[0] === "RULE-SET" && parts[1]) {
      usage[parts[1]] = (usage[parts[1]] || 0) + 1;
    }
  });
  return usage;
});

const providerItemsFromRecord = (
  record: unknown,
  kind: ProviderKind,
  usage: Record<string, number>,
): ProviderDisplayItem[] => {
  if (!isPlainObject(record)) return [];
  return Object.entries(record).map(([name, rawConfig]) => {
    const config = isPlainObject(rawConfig) ? rawConfig : {};
    return {
      kind,
      name,
      type: providerValue(config.type),
      url: providerValue(config.url),
      path: providerValue(config.path),
      behavior: providerValue(config.behavior),
      format: providerValue(config.format),
      interval: providerValue(config.interval),
      usageCount: usage[name] || 0,
      config,
    };
  });
};

const proxyProviderItems = computed<ProviderDisplayItem[]>(() =>
  providerItemsFromRecord(
    parsedConfigMap.value?.["proxy-providers"],
    "proxy",
    proxyProviderUsage.value,
  ),
);

const ruleProviderItems = computed<ProviderDisplayItem[]>(() =>
  providerItemsFromRecord(
    parsedConfigMap.value?.["rule-providers"],
    "rule",
    ruleProviderUsage.value,
  ),
);

const providerItems = computed<ProviderDisplayItem[]>(() => [
  ...proxyProviderItems.value,
  ...ruleProviderItems.value,
]);

const providerUsageLabel = (provider: ProviderDisplayItem) =>
  provider.kind === "proxy"
    ? `被 ${provider.usageCount} 个代理组引用`
    : `被 ${provider.usageCount} 条规则引用`;

const parseDiagnostics = computed<ParseDiagnostic[]>(() => {
  if (!result.value) return [];
  const diagnostics: ParseDiagnostic[] = [];

  if (!parsedConfig.value) {
    diagnostics.push({
      type: "error",
      title: "YAML 解析失败",
      description: "当前明文内容无法被前端解析为 YAML，因此只能展示原始文本。",
    });
    return diagnostics;
  }

  if (!parsedConfigMap.value) {
    diagnostics.push({
      type: "warning",
      title: "配置不是标准 YAML 映射",
      description: "当前结果不是以键值对形式组织的 Clash/Mihomo 配置，结构化面板暂无法展示。",
    });
    return diagnostics;
  }

  const nodesCount = topLevelArrayLength("proxies");
  const groupsCount = topLevelArrayLength("proxy-groups");
  const rulesCount = parsedRuleLines.value.length;

  if (nodesCount === 0 && groupsCount === 0 && rulesCount === 0 && providerItems.value.length === 0) {
    diagnostics.push({
      type: "warning",
      title: "未检测到可展示的结构化资源",
      description: "当前 YAML 顶层没有非空的 proxies、proxy-groups 或 rules 数组，所以节点、代理组和规则面板不会出现。",
    });
  }

  if (providerItems.value.length > 0) {
    diagnostics.push({
      type: "info",
      title: "检测到 Provider 型配置",
      description: "proxy-providers / rule-providers 已在 Provider 面板展示；具体节点通常由客户端根据 Provider URL 动态拉取。",
    });
  }

  if (isMultiSubscriptionProfile.value) {
    diagnostics.push({
      type: "info",
      title: "多订阅合并规则说明",
      description: "当前主订阅保留代理组和规则，其他订阅只合并顶层 proxies 节点；附加订阅的代理组和规则不会出现在结果中。",
    });
  }

  return diagnostics;
});

// 解析代理节点
const parsedNodes = computed<ProxyNode[]>(() => {
  const proxies = parsedConfigMap.value?.proxies;
  if (!Array.isArray(proxies)) return [];
  return proxies.filter(isPlainObject).map((p: any) => ({
    name: p.name,
    type: p.type,
    server: p.server,
    port: p.port,
    details: p,
  }));
});

// 代理组解析
const proxyGroups = computed<any[]>(() => {
  const groups = parsedConfigMap.value?.["proxy-groups"];
  return Array.isArray(groups) ? groups.filter(isPlainObject) : [];
});

const draggableNodes = ref<ProxyNode[]>([]);
const draggableGroups = ref<any[]>([]);
const dragSortState = ref<DragSortState | null>(null);
const dragOverState = ref<DragSortState | null>(null);
const isSavingNodeOrder = ref(false);
const isSavingGroupOrder = ref(false);

watch(
  parsedNodes,
  (nodes) => {
    draggableNodes.value = nodes.map((node) => ({ ...node }));
  },
  { immediate: true },
);

watch(
  proxyGroups,
  (groups) => {
    draggableGroups.value = groups.map((group) => ({
      ...group,
      proxies: Array.isArray(group.proxies) ? [...group.proxies] : [],
    }));
  },
  { immediate: true },
);

const isSavingResourceOrder = (resourceType: SortResourceType) =>
  resourceType === "nodes" ? isSavingNodeOrder.value : isSavingGroupOrder.value;

const setSavingResourceOrder = (resourceType: SortResourceType, saving: boolean) => {
  if (resourceType === "nodes") {
    isSavingNodeOrder.value = saving;
    return;
  }
  isSavingGroupOrder.value = saving;
};

const resourceOrderItems = (resourceType: SortResourceType) =>
  resourceType === "nodes" ? draggableNodes.value : draggableGroups.value;

const setResourceOrderItems = (resourceType: SortResourceType, items: any[]) => {
  if (resourceType === "nodes") {
    draggableNodes.value = items as ProxyNode[];
    return;
  }
  draggableGroups.value = items;
};

const handleSortDragStart = (
  event: DragEvent,
  resourceType: SortResourceType,
  fromIndex: number,
) => {
  if (isSavingResourceOrder(resourceType)) {
    event.preventDefault();
    return;
  }
  dragSortState.value = { resourceType, fromIndex };
  event.dataTransfer?.setData("text/plain", `${resourceType}:${fromIndex}`);
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = "move";
  }
};

const handleSortDragOver = (
  event: DragEvent,
  resourceType: SortResourceType,
  index: number,
) => {
  if (
    !dragSortState.value ||
    dragSortState.value.resourceType !== resourceType ||
    isSavingResourceOrder(resourceType)
  ) {
    return;
  }
  event.preventDefault();
  dragOverState.value = { resourceType, fromIndex: index };
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "move";
  }
};

const handleSortDrop = async (
  event: DragEvent,
  resourceType: SortResourceType,
  toIndex: number,
) => {
  event.preventDefault();
  const state = dragSortState.value;
  dragSortState.value = null;
  dragOverState.value = null;
  if (!state || state.resourceType !== resourceType || isSavingResourceOrder(resourceType)) {
    return;
  }
  await reorderDisplayItems(resourceType, state.fromIndex, toIndex);
};

const handleSortDragEnd = () => {
  dragSortState.value = null;
  dragOverState.value = null;
};

const isDragOverItem = (resourceType: SortResourceType, index: number) =>
  dragOverState.value?.resourceType === resourceType &&
  dragOverState.value?.fromIndex === index;

const reorderDisplayItems = async (
  resourceType: SortResourceType,
  fromIndex: number,
  toIndex: number,
) => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }

  const currentItems = resourceOrderItems(resourceType);
  if (
    fromIndex === toIndex ||
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= currentItems.length ||
    toIndex >= currentItems.length
  ) {
    return;
  }

  const previousItems = currentItems.map((item) => ({ ...item }));
  const nextItems = currentItems.map((item) => ({ ...item }));
  const [moved] = nextItems.splice(fromIndex, 1);
  nextItems.splice(toIndex, 0, moved);
  setResourceOrderItems(resourceType, nextItems);
  setSavingResourceOrder(resourceType, true);

  try {
    const names = nextItems.map((item) => item.name).filter(Boolean);
    const res = await axios.put(profileScopedUrl("/api/resource-orders"), {
      profile_id: activeProfileId.value,
      resource_type: resourceType,
      names,
    });
    if (res.data.code !== 200) {
      throw new Error(res.data.message || "排序保存失败");
    }
    ElMessage.success(resourceType === "nodes" ? "节点排序已保存" : "代理组排序已保存");
    await loadSubscription({ preferredTab: resourceType === "nodes" ? "nodes" : "groups" });
  } catch (error: any) {
    setResourceOrderItems(resourceType, previousItems);
    ElMessage.error(
      "排序保存失败: " + (error.response?.data?.message || error.message),
    );
  } finally {
    setSavingResourceOrder(resourceType, false);
  }
};

// 规则搜索关键字
const ruleSearchQuery = ref("");

// 分流规则目标策略筛选
const ruleTargetFilter = ref("");
const bulkRuleTarget = ref("");

const builtInRuleTargetOptions = [
  { label: "DIRECT (直连)", value: "DIRECT" },
  { label: "REJECT (拒绝)", value: "REJECT" },
  { label: "PROXY (默认代理)", value: "PROXY" },
];

interface RuleDisplayRow {
  raw: string;
  type: string;
  payload: string;
  target: string;
}

interface RuleWritePayload {
  type: string;
  payload: string;
  target: string;
}

type RuleIdentity = Pick<RuleWritePayload, "type" | "payload">;

const deletedCustomRuleTarget = "__DELETE__";

const normalizeRulePayloadForSubmit = (payload: string) => {
  const normalized = String(payload || "").trim();
  return normalized === "" || normalized === "-" ? "-" : normalized;
};

const normalizeRuleTypeForSubmit = (type: string) => String(type || "").trim().toUpperCase();

const normalizeRuleTargetForSubmit = (target: string) => String(target || "").trim();

const getRuleIdentityKey = (row: RuleIdentity) => {
  const type = normalizeRuleTypeForSubmit(row.type);
  const payload = normalizeRulePayloadForSubmit(row.payload);
  return payload === "-" ? type : `${type},${payload}`;
};

const buildRuleWritePayload = (rule: RuleWritePayload): RuleWritePayload => ({
  type: normalizeRuleTypeForSubmit(rule.type),
  payload: normalizeRulePayloadForSubmit(rule.payload),
  target: normalizeRuleTargetForSubmit(rule.target),
});

const parseRuleForDisplay = (ruleStr: string) => {
  const parts = String(ruleStr).split(",").map((part) => part.trim());
  const type = parts[0] || "UNKNOWN";
  const noPayloadRule = ["MATCH", "FINAL"].includes(type.toUpperCase());
  if (noPayloadRule || parts.length <= 2) {
    return {
      raw: ruleStr,
      type,
      payload: "-",
      target: parts.slice(1).join(",") || "-",
    };
  }
  const optionSuffixes = new Set(["no-resolve"]);
  let targetStart = parts.length - 1;
  if (parts.length >= 4 && optionSuffixes.has(parts[parts.length - 1].toLowerCase())) {
    targetStart = parts.length - 2;
  }
  return {
    raw: ruleStr,
    type,
    payload: parts.slice(1, targetStart).join(",") || "-",
    target: parts.slice(targetStart).join(",") || "-",
  };
};

const ruleTargets = computed(() => {
  const targets = new Set<string>();
  parsedRuleLines.value.forEach((ruleStr: string) => {
    targets.add(parseRuleForDisplay(ruleStr).target);
  });
  return Array.from(targets).sort();
});

// 规则分页状态
const currentRulePage = ref(1);
const rulePageSize = ref(100);

// 监听搜索词或筛选变化，重置页码
watch([ruleSearchQuery, ruleTargetFilter], () => {
  currentRulePage.value = 1;
});

// 分流规则解析 (拆解 DOMAIN-SUFFIX,google.com,PROXY)
const parsedRules = computed<RuleDisplayRow[]>(() => {
  let rules = parsedRuleLines.value.map((ruleStr: string) => parseRuleForDisplay(ruleStr));

  if (ruleTargetFilter.value) {
    rules = rules.filter((r: any) => r.target === ruleTargetFilter.value);
  }

  if (ruleSearchQuery.value) {
    const q = ruleSearchQuery.value.toLowerCase();
    rules = rules.filter(
      (r: any) =>
        r.type.toLowerCase().includes(q) ||
        r.payload.toLowerCase().includes(q) ||
        r.target.toLowerCase().includes(q),
    );
  }

  return rules;
});

// 当前页显示的规则
const paginatedRules = computed(() => {
  const start = (currentRulePage.value - 1) * rulePageSize.value;
  const end = start + rulePageSize.value;
  return parsedRules.value.slice(start, end);
});

// 根据节点名称自适应匹配国旗表情包
const getFlagEmoji = (name: string): string => {
  const n = name.toUpperCase();
  if (n.includes("香港") || n.includes("HK") || n.includes("HONGKONG"))
    return "🇭🇰";
  if (n.includes("新加坡") || n.includes("SG") || n.includes("SINGAPORE"))
    return "🇸🇬";
  if (
    n.includes("日本") ||
    n.includes("东京") ||
    n.includes("JP") ||
    n.includes("JAPAN") ||
    n.includes("TOKYO")
  )
    return "🇯🇵";
  if (
    n.includes("美国") ||
    n.includes("US") ||
    n.includes("UNITED STATES") ||
    n.includes("美")
  )
    return "🇺🇸";
  if (n.includes("台湾") || n.includes("TW") || n.includes("TAIWAN"))
    return "🇹🇼";
  if (
    n.includes("韩国") ||
    n.includes("首尔") ||
    n.includes("KR") ||
    n.includes("KOREA")
  )
    return "🇰🇷";
  if (
    n.includes("英国") ||
    n.includes("UK") ||
    n.includes("GB") ||
    n.includes("ENGLAND")
  )
    return "🇬🇧";
  if (n.includes("德国") || n.includes("DE") || n.includes("GERMANY"))
    return "🇩🇪";
  if (n.includes("俄罗斯") || n.includes("RU") || n.includes("RUSSIA"))
    return "🇷🇺";
  return "🌐";
};

// 节点协议色彩主题
const getNodeTypeTag = (type: string) => {
  const t = type.toLowerCase();
  if (t === "ss" || t === "shadowsocks")
    return { type: "warning", label: "SS" };
  if (t === "vmess") return { type: "danger", label: "VMESS" };
  if (t === "vless") return { type: "success", label: "VLESS" };
  if (t === "trojan") return { type: "primary", label: "TROJAN" };
  if (t === "ssr" || t === "shadowsocksr")
    return { type: "info", label: "SSR" };
  return { type: "info", label: type.toUpperCase() };
};

// 统计信息
const stats = computed(() => {
  if (!result.value) return { size: 0, lines: 0 };
  const size = new Blob([result.value.decoded]).size;
  const lines = result.value.decoded.split("\n").length;
  return { size, lines };
});

// 一键复制
const handleCopy = async () => {
  if (!result.value) return;
  try {
    await navigator.clipboard.writeText(result.value.decoded);
    ElMessage.success("配置内容已成功复制到剪贴板！");
  } catch (err) {
    ElMessage.error("复制失败，请手动选择复制");
  }
};

// 导出下载文件
const handleDownload = () => {
  if (!result.value) return;
  const blob = new Blob([result.value.decoded], {
    type: "text/plain;charset=utf-8",
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `clash_decoded_${Date.now()}.yaml`;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
  ElMessage.success("成功导出本地配置文件！");
};

// ---------------------- 自定义组管理逻辑 ----------------------
const groupDialogVisible = ref(false);
const isSubmittingGroup = ref(false);
const editingGroupId = ref<number | null>(null);
const copyGroupsDialogVisible = ref(false);
const isCopyingGroups = ref(false);
const copyGroupsSourceProfileId = ref<number | null>(null);

const newGroupForm = ref({
  name: "",
  type: "select",
  proxies: [] as string[],
  exclude: "",
  shadowrocket_use_builtin_proxy: false,
});

const groupTypes = ["select", "url-test", "fallback", "load-balance"];
const builtInGroupProxies = [
  { label: "DIRECT (直连)", value: "DIRECT" },
  { label: "REJECT (拒绝)", value: "REJECT" },
];

const copyGroupSourceOptions = computed(() =>
  profiles.value.filter((profile) => profile.id !== activeProfileId.value),
);

const openGroupDialog = () => {
  editingGroupId.value = null;
  newGroupForm.value = {
    name: "",
    type: "select",
    proxies: [],
    exclude: "",
    shadowrocket_use_builtin_proxy: false,
  };
  groupDialogVisible.value = true;
};

const openCopyGroupsDialog = () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  copyGroupsSourceProfileId.value = copyGroupSourceOptions.value[0]?.id || null;
  copyGroupsDialogVisible.value = true;
};

const subscriptionResourcePayload = (
  resourceType: SubscriptionResourceType,
  name: string,
  data: Record<string, any>,
) => ({
  profile_id: activeProfileId.value,
  resource_type: resourceType,
  name,
  data,
});

const copyGroupsFromProfile = async () => {
  if (!activeProfileId.value || !copyGroupsSourceProfileId.value) {
    ElMessage.warning("请选择来源配置");
    return;
  }
  isCopyingGroups.value = true;
  try {
    const res = await axios.post(
      buildBackendUrl(`/api/profiles/${activeProfileId.value}/copy-groups`),
      { source_profile_id: copyGroupsSourceProfileId.value },
    );
    if (res.data.code === 200) {
      ElMessage.success(`代理组复制成功，来源代理组 ${res.data.data?.copied || 0} 个`);
      copyGroupsDialogVisible.value = false;
      await fetchCustomData();
      await loadSubscription({ preferredTab: "groups" });
    } else {
      ElMessage.error(res.data.message || "复制代理组失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "复制代理组失败");
  } finally {
    isCopyingGroups.value = false;
  }
};

const openCustomGroupEditor = (groupName: string) => {
  const customInfo = customGroupsDict.value[groupName];
  if (!customInfo) return;
  editingGroupId.value = customInfo.ID;
  let proxiesList: string[] = [];
  try {
    proxiesList = JSON.parse(customInfo.Proxies || "[]");
  } catch (e) {}
  newGroupForm.value = {
    name: customInfo.Name || customInfo.name,
    type: customInfo.Type || customInfo.type,
    proxies: proxiesList,
    exclude: customInfo.Exclude || customInfo.exclude || "",
    shadowrocket_use_builtin_proxy: Boolean(
      customInfo.shadowrocket_use_builtin_proxy ||
        customInfo.ShadowrocketUseBuiltinProxy ||
        customInfo.shadowrocketUseBuiltinProxy,
    ),
  };
  groupDialogVisible.value = true;
};

const editCustomGroup = async (group: any) => {
  const groupName = group.name;
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  if (customGroupsDict.value[groupName]) {
    openCustomGroupEditor(groupName);
    return;
  }
  try {
    const res = await axios.post(
      buildBackendUrl("/api/subscription-resources/takeover"),
      subscriptionResourcePayload("groups", groupName, group),
    );
    if (res.data.code === 200) {
      ElMessage.success("订阅代理组已接管为自定义代理组，可继续编辑");
      await fetchCustomData();
      const takenOverName = res.data.data?.Name || res.data.data?.name || groupName;
      openCustomGroupEditor(takenOverName);
    } else {
      ElMessage.error(res.data.message || "接管代理组失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "接管代理组失败");
  }
};

const deleteCustomGroup = (group: any) => {
  const groupName = group.name;
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  const isCustom = Boolean(customGroupsDict.value[groupName]);
  ElMessageBox.confirm(
    isCustom
      ? `确定要删除自定义策略组 [${groupName}] 吗？`
      : `确定要在当前配置中删除订阅策略组 [${groupName}] 吗？刷新订阅后也不会自动恢复。`,
    "安全提示",
    {
      confirmButtonText: "确定删除",
      cancelButtonText: "取消",
      type: "warning",
    },
  )
    .then(async () => {
      try {
        const res = await axios.post(
          buildBackendUrl("/api/subscription-resources/delete"),
          subscriptionResourcePayload("groups", groupName, group),
        );
        if (res.data.code === 200) {
          ElMessage.success(isCustom ? "自定义策略组已成功删除！" : "订阅策略组已从当前配置隐藏！");
          await fetchCustomData();
          if (activeProfileId.value) {
            await loadSubscription({ preferredTab: "groups" });
          }
        }
      } catch (err: any) {
        ElMessage.error("删除失败: " + err.message);
      }
    })
    .catch(() => {});
};

const selectAllNodes = () => {
  if (!newGroupForm.value.proxies.includes("[ALL_NODES]")) {
    newGroupForm.value.proxies.push("[ALL_NODES]");
  }
};

const selectAllExistingGroups = () => {
  const currentGroups = proxyGroups.value.map((g) => g.name);
  for (const g of currentGroups) {
    if (!newGroupForm.value.proxies.includes(g)) {
      newGroupForm.value.proxies.push(g);
    }
  }
};

const selectDirectPolicy = () => {
  if (!newGroupForm.value.proxies.includes("DIRECT")) {
    newGroupForm.value.proxies.push("DIRECT");
  }
};

const selectRejectPolicy = () => {
  if (!newGroupForm.value.proxies.includes("REJECT")) {
    newGroupForm.value.proxies.push("REJECT");
  }
};

const clearGroupProxies = () => {
  newGroupForm.value.proxies = [];
};

const isEnabledFlag = (value: unknown) =>
  value === true ||
  value === 1 ||
  value === "1" ||
  (typeof value === "string" && value.toLowerCase() === "true");

const isShadowrocketBuiltinProxyGroup = (group: any) => {
  const groupName = String(group?.name || group?.Name || "").trim();
  if (!groupName) return false;
  const customInfo = customGroupsDict.value[groupName];
  if (!customInfo) return false;
  return (
    isEnabledFlag(customInfo.shadowrocket_use_builtin_proxy) ||
    isEnabledFlag(customInfo.ShadowrocketUseBuiltinProxy) ||
    isEnabledFlag(customInfo.shadowrocketUseBuiltinProxy)
  );
};

const saveCustomGroup = async () => {
  if (!newGroupForm.value.name) {
    ElMessage.warning("请输入策略组名称");
    return;
  }
  if (newGroupForm.value.proxies.length === 0) {
    ElMessage.warning("请至少选择一个代理或节点");
    return;
  }
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }

  isSubmittingGroup.value = true;
  try {
    let res;
    const payload = { ...newGroupForm.value, profile_id: activeProfileId.value };
    if (editingGroupId.value) {
      res = await axios.put(
        buildBackendUrl(`/api/custom-groups/${editingGroupId.value}`),
        payload,
      );
    } else {
      res = await axios.post(
        buildBackendUrl("/api/custom-groups"),
        payload,
      );
    }
    if (res.data.code === 200) {
      ElMessage.success(
        editingGroupId.value
          ? "自定义组更新成功！"
          : "自定义组已云端保存成功！",
      );
      groupDialogVisible.value = false;
      await fetchCustomData();
      if (activeProfileId.value) {
        await loadSubscription({ preferredTab: "groups" });
      }
    } else {
      throw new Error(res.data.message);
    }
  } catch (error: any) {
    ElMessage.error(
      "保存失败: " + (error.response?.data?.message || error.message),
    );
  } finally {
    isSubmittingGroup.value = false;
  }
};

// ---------------------- 自定义节点管理逻辑 ----------------------
const nodeDialogVisible = ref(false);
const nodeActiveTab = ref("link");
const isParsingLink = ref(false);
const isSubmittingNode = ref(false);
const editingNodeId = ref<number | null>(null);

const nodeLinkForm = ref({
  link: "",
});

const newNodeForm = ref({
  name: "",
  type: "vless",
  server: "",
  port: 443,
  config: {} as Record<string, any>,
});

const nodeTypes = ["vless", "hysteria2", "ss", "vmess", "trojan", "socks5"];

const configString = computed({
  get: () => JSON.stringify(newNodeForm.value.config, null, 2),
  set: (val: string) => {
    try {
      newNodeForm.value.config = JSON.parse(val);
    } catch (e) {
      // ignore
    }
  },
});

const openNodeDialog = () => {
  editingNodeId.value = null;
  nodeLinkForm.value.link = "";
  newNodeForm.value = {
    name: "",
    type: "vless",
    server: "",
    port: 443,
    config: {},
  };
  nodeDialogVisible.value = true;
  nodeActiveTab.value = "link";
};

const openCustomNodeEditor = (nodeName: string) => {
  const customInfo = customNodesDict.value[nodeName];
  if (!customInfo) return;
  editingNodeId.value = customInfo.ID;
  let configMap: Record<string, any> = {};
  try {
    configMap = JSON.parse(customInfo.Config || "{}");
  } catch (e) {}
  newNodeForm.value = {
    name: customInfo.Name || customInfo.name,
    type: customInfo.Type || customInfo.type,
    server: customInfo.Server || customInfo.server,
    port: customInfo.Port || customInfo.port,
    config: configMap,
  };
  nodeActiveTab.value = "manual";
  nodeDialogVisible.value = true;
};

const editCustomNode = async (node: ProxyNode) => {
  const nodeName = node.name;
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  if (customNodesDict.value[nodeName]) {
    openCustomNodeEditor(nodeName);
    return;
  }
  try {
    const res = await axios.post(
      buildBackendUrl("/api/subscription-resources/takeover"),
      subscriptionResourcePayload("nodes", nodeName, node.details || {}),
    );
    if (res.data.code === 200) {
      ElMessage.success("订阅节点已接管为自定义节点，可继续编辑");
      await fetchCustomData();
      const takenOverName = res.data.data?.Name || res.data.data?.name || nodeName;
      openCustomNodeEditor(takenOverName);
    } else {
      ElMessage.error(res.data.message || "接管节点失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "接管节点失败");
  }
};

const deleteCustomNode = (node: ProxyNode) => {
  const nodeName = node.name;
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  const isCustom = Boolean(customNodesDict.value[nodeName]);
  ElMessageBox.confirm(
    isCustom
      ? `确定要彻底删除自定义节点 [${nodeName}] 吗？`
      : `确定要在当前配置中删除订阅节点 [${nodeName}] 吗？刷新订阅后也不会自动恢复。`,
    "安全提示",
    {
      confirmButtonText: "立即销毁",
      cancelButtonText: "取消保留",
      type: "warning",
    },
  )
    .then(async () => {
      try {
        const res = await axios.post(
          buildBackendUrl("/api/subscription-resources/delete"),
          subscriptionResourcePayload("nodes", nodeName, node.details || {}),
        );
        if (res.data.code === 200) {
          ElMessage.success(isCustom ? "自定义节点已被彻底删除！" : "订阅节点已从当前配置隐藏！");
          await fetchCustomData();
          if (activeProfileId.value) {
            await loadSubscription({ preferredTab: "nodes" });
          }
        }
      } catch (err: any) {
        ElMessage.error("删除节点失败: " + err.message);
      }
    })
    .catch(() => {});
};

const parseNodeLink = async () => {
  if (!nodeLinkForm.value.link) {
    ElMessage.warning("请输入节点链接");
    return;
  }
  isParsingLink.value = true;
  try {
    const res = await axios.post(buildBackendUrl("/api/parse-link"), {
      link: nodeLinkForm.value.link,
    });
    if (res.data.code === 200) {
      const data = res.data.data;
      newNodeForm.value.name = data.name || "";
      newNodeForm.value.type = data.type || "vless";
      newNodeForm.value.server = data.server || "";
      newNodeForm.value.port = data.port || 443;
      newNodeForm.value.config = data.config || {};
      ElMessage.success("链接解析成功！请在右侧检查参数");
      nodeActiveTab.value = "manual";
    } else {
      throw new Error(res.data.message);
    }
  } catch (error: any) {
    ElMessage.error(
      "解析失败: " + (error.response?.data?.message || error.message),
    );
  } finally {
    isParsingLink.value = false;
  }
};

const saveCustomNode = async () => {
  if (
    !newNodeForm.value.name ||
    !newNodeForm.value.server ||
    !newNodeForm.value.port
  ) {
    ElMessage.warning("请补全基础信息（名称、服务器、端口）");
    return;
  }
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  isSubmittingNode.value = true;

  // 同步基础信息到 config
  newNodeForm.value.config.name = newNodeForm.value.name;
  newNodeForm.value.config.type = newNodeForm.value.type;
  newNodeForm.value.config.server = newNodeForm.value.server;
  newNodeForm.value.config.port = newNodeForm.value.port;

  try {
    let res;
    const payload = { ...newNodeForm.value, profile_id: activeProfileId.value };
    if (editingNodeId.value) {
      res = await axios.put(
        buildBackendUrl(`/api/custom-nodes/${editingNodeId.value}`),
        payload,
      );
    } else {
      res = await axios.post(
        buildBackendUrl("/api/custom-nodes"),
        payload,
      );
    }
    if (res.data.code === 200) {
      ElMessage.success(
        editingNodeId.value
          ? "自定义节点更新成功！"
          : "自定义节点云端保存成功！",
      );
      nodeDialogVisible.value = false;
      await fetchCustomData();
      if (activeProfileId.value) {
        await loadSubscription({ preferredTab: "nodes" });
      }
    } else {
      throw new Error(res.data.message);
    }
  } catch (error: any) {
    ElMessage.error(
      "保存失败: " + (error.response?.data?.message || error.message)
    );
  } finally {
    isSubmittingNode.value = false;
  }
};

const dirtyRulesMap = ref<Record<string, any>>({});
const dirtyRuleCount = computed(() => Object.keys(dirtyRulesMap.value).length);
const hasDirtyRules = computed(() => dirtyRuleCount.value > 0);
const isDeletingFilteredRules = ref(false);
const hasFilteredRules = computed(() => parsedRules.value.length > 0);
const canDeleteFilteredRules = computed(
  () =>
    Boolean(ruleTargetFilter.value) &&
    hasFilteredRules.value &&
    !hasDirtyRules.value &&
    !isDeletingFilteredRules.value,
);

const markRuleDirty = (row: any) => {
  dirtyRulesMap.value[getRuleIdentityKey(row)] = row;
};

interface BatchRuleProgress {
  stage: "saving" | "reapplying" | "complete";
  current: number;
  total: number;
  saved?: number;
  message?: string;
}

interface ParsedSSEMessage {
  event: string;
  data: string;
}

let batchRulesLoading: ReturnType<typeof ElLoading.service> | null = null;

const parseSSEMessage = (rawMessage: string): ParsedSSEMessage | null => {
  const lines = rawMessage.split("\n");
  const dataLines: string[] = [];
  let event = "message";

  for (const line of lines) {
    if (line.startsWith("event:")) {
      event = line.slice("event:".length).trim();
      continue;
    }
    if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }

  if (dataLines.length === 0) return null;
  return { event, data: dataLines.join("\n") };
};

const updateBatchRulesLoading = (progress: BatchRuleProgress) => {
  const fallbackText =
    progress.stage === "reapplying"
      ? "规则已保存，正在重新应用订阅配置"
      : `批量应用规则中：${progress.current}/${progress.total}`;
  batchRulesLoading?.setText(progress.message || fallbackText);
};

const readBatchRulesSSE = async (
  response: Response,
  onProgress: (progress: BatchRuleProgress) => void,
): Promise<BatchRuleProgress> => {
  const reader = response.body?.getReader();
  if (!reader) {
    throw new Error("当前浏览器不支持读取批量保存进度流");
  }

  const decoder = new TextDecoder();
  let buffer = "";
  let completeProgress: BatchRuleProgress | null = null;

  const handleRawMessage = (rawMessage: string) => {
    const parsed = parseSSEMessage(rawMessage.trim());
    if (!parsed) return;

    const data = JSON.parse(parsed.data);
    if (parsed.event === "error") {
      throw new Error(data.message || "批量保存规则失败");
    }
    if (parsed.event === "progress") {
      onProgress(data as BatchRuleProgress);
      return;
    }
    if (parsed.event === "complete") {
      const progress = data as BatchRuleProgress;
      completeProgress = progress;
      onProgress(progress);
    }
  };

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    buffer = buffer.replace(/\r\n/g, "\n");

    let boundaryIndex = buffer.indexOf("\n\n");
    while (boundaryIndex >= 0) {
      const rawMessage = buffer.slice(0, boundaryIndex);
      buffer = buffer.slice(boundaryIndex + 2);
      handleRawMessage(rawMessage);
      boundaryIndex = buffer.indexOf("\n\n");
    }
  }

  buffer += decoder.decode();
  if (buffer.trim()) {
    handleRawMessage(buffer);
  }

  if (!completeProgress) {
    throw new Error("批量保存进度流异常结束");
  }
  return completeProgress;
};

const applyBulkRuleTargetToFilteredRules = () => {
  const nextTarget = bulkRuleTarget.value.trim();
  if (!nextTarget) {
    ElMessage.warning("请选择要批量设置的目标策略");
    return;
  }

  const filteredRules = parsedRules.value;
  if (filteredRules.length === 0) {
    ElMessage.warning("当前筛选结果为空，无法批量设置");
    return;
  }

  let changedCount = 0;
  filteredRules.forEach((row) => {
    if (row.target === nextTarget) return;
    row.target = nextTarget;
    markRuleDirty(row);
    changedCount += 1;
  });

  if (changedCount === 0) {
    ElMessage.info("当前筛选结果已全部使用该目标策略");
    return;
  }

  ElMessage.success(`已将当前筛选的 ${changedCount} 条规则设置为 ${nextTarget}，请点击批量应用修改保存`);
};

const batchSaveRules = async () => {
  if (!hasDirtyRules.value) return;
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  isSubmittingRule.value = true;
  
  try {
    const rules = Object.values(dirtyRulesMap.value).map((row: any) => ({
      type: row.type,
      payload: row.payload === "-" ? "-" : row.payload,
      target: row.target,
    }));

    batchRulesLoading = ElLoading.service({
      lock: true,
      text: `批量应用规则中：0/${rules.length}`,
      background: "rgba(10, 14, 24, 0.72)",
    });

    const token = localStorage.getItem("token");
    const response = await fetch(buildBackendUrl("/api/custom-rules/batch/stream"), {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({
        profile_id: activeProfileId.value,
        rules,
      }),
    });

    if (response.status === 401) {
      localStorage.removeItem("token");
      window.dispatchEvent(new CustomEvent("auth-failed"));
      throw new Error("登录状态已失效，请重新登录");
    }
    if (!response.ok) {
      const errorText = await response.text();
      throw new Error(errorText || "批量保存规则失败");
    }

    const completeProgress = await readBatchRulesSSE(response, updateBatchRulesLoading);
    const savedCount = completeProgress.saved || completeProgress.total || rules.length;
    ElMessage.success(`成功批量接管 ${savedCount} 条策略！正在重新拉取订阅...`);
    dirtyRulesMap.value = {};
    await fetchCustomData();
    if (activeProfileId.value) {
      await loadSubscription({ preferredTab: "rules" });
    }
  } catch (error: any) {
    ElMessage.error("部分策略保存失败: " + (error.response?.data?.message || error.message));
  } finally {
    batchRulesLoading?.close();
    batchRulesLoading = null;
    isSubmittingRule.value = false;
  }
};

const deleteFilteredRulesByTarget = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  if (!ruleTargetFilter.value) {
    ElMessage.warning("请先选择要删除的目标策略");
    return;
  }
  if (hasDirtyRules.value) {
    ElMessage.warning("当前存在未保存的规则修改，请先批量应用修改后再删除");
    return;
  }

  const rules = parsedRules.value.map((row) =>
    buildRuleWritePayload({
      type: row.type,
      payload: row.payload,
      target: row.target,
    }),
  );
  if (rules.length === 0) {
    ElMessage.warning("当前筛选结果为空，无法删除");
    return;
  }

  try {
    await ElMessageBox.confirm(
      `确定要删除目标策略「${ruleTargetFilter.value}」下当前筛选出的 ${rules.length} 条规则吗？删除后刷新订阅也不会恢复这些规则。`,
      "批量删除规则确认",
      {
        confirmButtonText: "确认删除",
        cancelButtonText: "取消",
        type: "warning",
        customClass: "glass-dialog",
      },
    );
  } catch {
    return;
  }

  isDeletingFilteredRules.value = true;
  try {
    const deletedCount = await persistRuleDeletions(rules);
    ElMessage.success(`已删除 ${deletedCount} 条规则，正在刷新规则列表...`);
    ruleTargetFilter.value = "";
    dirtyRulesMap.value = {};
    await fetchCustomData();
    await loadSubscription({ preferredTab: "rules" });
  } catch (error: any) {
    ElMessage.error("批量删除失败: " + (error.response?.data?.message || error.message));
  } finally {
    isDeletingFilteredRules.value = false;
  }
};

// ---------------------- 自定义规则管理逻辑 ----------------------
const ruleDialogVisible = ref(false);
const isSubmittingRule = ref(false);
const editingRuleId = ref<number | null>(null);
const editingOriginalRuleIdentity = ref<RuleIdentity | null>(null);
const copyRulesDialogVisible = ref(false);
const isCopyingRules = ref(false);
const isLocalizingRules = ref(false);
const copyRulesSourceProfileId = ref<number | null>(null);

const newRuleForm = ref({
  type: "DOMAIN-SUFFIX",
  payload: "",
  target: "PROXY",
});

const ruleTypes = [
  "DOMAIN-SUFFIX",
  "DOMAIN-KEYWORD",
  "DOMAIN",
  "IP-CIDR",
  "IP-CIDR6",
  "GEOSITE",
  "GEOIP",
  "MATCH",
  "FINAL",
  "PROCESS-NAME",
];

const copyRuleSourceOptions = computed(() =>
  profiles.value.filter((profile) => profile.id !== activeProfileId.value),
);

const openCopyRulesDialog = () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  copyRulesSourceProfileId.value = copyRuleSourceOptions.value[0]?.id || null;
  copyRulesDialogVisible.value = true;
};

const copyRulesFromProfile = async () => {
  if (!activeProfileId.value || !copyRulesSourceProfileId.value) {
    ElMessage.warning("请选择来源配置");
    return;
  }
  isCopyingRules.value = true;
  try {
    const res = await axios.post(
      buildBackendUrl(`/api/profiles/${activeProfileId.value}/copy-rules`),
      { source_profile_id: copyRulesSourceProfileId.value },
    );
    if (res.data.code === 200) {
      ElMessage.success(`规则复制成功，来源规则 ${res.data.data?.copied || 0} 条`);
      copyRulesDialogVisible.value = false;
      await fetchCustomData();
      await loadSubscription({ preferredTab: "rules" });
    } else {
      ElMessage.error(res.data.message || "复制规则失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.message || "复制规则失败");
  } finally {
    isCopyingRules.value = false;
  }
};

const localizeRemoteRules = async () => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }
  if (currentProfile.value?.source_type !== "remote") {
    ElMessage.warning("只有远程订阅配置可以本地化远程规则");
    return;
  }

  isLocalizingRules.value = true;
  try {
    const res = await axios.post(
      buildBackendUrl(`/api/profiles/${activeProfileId.value}/localize-rules`),
    );
    if (res.data.code === 200) {
      const data = res.data.data || {};
      ElMessage.success(
        `远程规则本地化完成，新增 ${data.imported || 0} 条，跳过 ${data.skipped_existing || 0} 条`,
      );
      dirtyRulesMap.value = {};
      await fetchCustomData();
      await loadSubscription({ preferredTab: "rules" });
    } else {
      ElMessage.error(res.data.message || "本地化远程规则失败");
    }
  } catch (err: any) {
    ElMessage.error(err.response?.data?.error || err.response?.data?.message || "本地化远程规则失败");
  } finally {
    isLocalizingRules.value = false;
  }
};

const handleRuleToolbarCommand = (command: string) => {
  if (command === "localizeRemoteRules") {
    void localizeRemoteRules();
    return;
  }
  if (command === "copyRules") {
    openCopyRulesDialog();
    return;
  }
  if (command === "deleteFilteredRules") {
    if (!canDeleteFilteredRules.value) return;
    void deleteFilteredRulesByTarget();
  }
};

const openRuleDialog = () => {
  editingRuleId.value = null;
  editingOriginalRuleIdentity.value = null;
  newRuleForm.value = { type: "DOMAIN-SUFFIX", payload: "", target: "PROXY" };
  ruleDialogVisible.value = true;
};

const editRule = (row: any) => {
  let key = getRuleIdentityKey(row);
  let customInfo = customRulesDict.value[key];

  if (customInfo && customInfo.Target !== deletedCustomRuleTarget) {
    editingRuleId.value = customInfo.ID;
    editingOriginalRuleIdentity.value = {
      type: customInfo.Type,
      payload: customInfo.Payload,
    };
    newRuleForm.value = {
      type: customInfo.Type,
      payload: customInfo.Payload === "-" ? "" : customInfo.Payload,
      target: customInfo.Target,
    };
  } else {
    editingRuleId.value = null; // 接管原生规则
    editingOriginalRuleIdentity.value = {
      type: row.type,
      payload: row.payload,
    };
    newRuleForm.value = {
      type: row.type,
      payload: row.payload === "-" ? "" : row.payload,
      target: row.target,
    };
  }
  ruleDialogVisible.value = true;
};

const persistRuleDeletions = async (rules: RuleWritePayload[]) => {
  if (!activeProfileId.value) {
    throw new Error("请先选择一个配置");
  }
  const res = await axios.post(buildBackendUrl("/api/custom-rules/batch-delete"), {
    profile_id: activeProfileId.value,
    rules: rules.map(buildRuleWritePayload),
  });
  if (res.data.code !== 200) {
    throw new Error(res.data.message || "规则删除失败");
  }
  return res.data.data?.deleted || rules.length;
};

const removeCustomRuleOverride = (row: any) => {
  let key = getRuleIdentityKey(row);
  let customInfo = customRulesDict.value[key];
  if (!customInfo || customInfo.Target === deletedCustomRuleTarget) return;

  ElMessageBox.confirm(`确定要移除对该规则的接管吗？订阅中原本存在的同名规则会恢复显示。`, "移除接管确认", {
    confirmButtonText: "确认移除",
    cancelButtonText: "取消",
    type: "warning",
  })
    .then(async () => {
      try {
        const res = await axios.delete(
          buildBackendUrl(`/api/custom-rules/${customInfo.ID}`)
        );
        if (res.data.code === 200) {
          ElMessage.success("规则接管已移除！");
          await fetchCustomData();
          if (activeProfileId.value) {
            await loadSubscription({ preferredTab: "rules" });
          }
        }
      } catch (err: any) {
        ElMessage.error("移除失败: " + err.message);
      }
    })
    .catch(() => {});
};

const deleteRule = async (row: RuleDisplayRow) => {
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }

  const pendingChangeTip = hasDirtyRules.value ? "当前未保存的规则修改会在删除成功后清空。" : "";
  try {
    await ElMessageBox.confirm(
      `确定要删除这条分流规则吗？删除后刷新订阅也不会恢复。${pendingChangeTip}`,
      "删除规则确认",
      {
        confirmButtonText: "确认删除",
        cancelButtonText: "取消",
        type: "warning",
        customClass: "glass-dialog",
      },
    );
  } catch {
    return;
  }

  isSubmittingRule.value = true;
  try {
    const deletedCount = await persistRuleDeletions([
      buildRuleWritePayload({
        type: row.type,
        payload: row.payload,
        target: row.target,
      }),
    ]);
    ElMessage.success(`已删除 ${deletedCount} 条规则，正在刷新规则列表...`);
    dirtyRulesMap.value = {};
    await fetchCustomData();
    await loadSubscription({ preferredTab: "rules" });
  } catch (error: any) {
    ElMessage.error("删除失败: " + (error.response?.data?.message || error.message));
  } finally {
    isSubmittingRule.value = false;
  }
};

const saveCustomRule = async () => {
  if (!newRuleForm.value.type || !newRuleForm.value.target) {
    ElMessage.warning("请补全规则类型和目标策略");
    return;
  }
  const normalizedRuleType = normalizeRuleTypeForSubmit(newRuleForm.value.type).toUpperCase();
  const normalizedPayload = normalizeRulePayloadForSubmit(newRuleForm.value.payload);
  if (!["MATCH", "FINAL"].includes(normalizedRuleType) && normalizedPayload === "-") {
    ElMessage.warning("请输入匹配内容 (Payload)");
    return;
  }
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }

  const submitRule = buildRuleWritePayload(newRuleForm.value);
  let submitData = { ...submitRule, profile_id: activeProfileId.value };
  const originalIdentity = editingOriginalRuleIdentity.value;
  const isReplacingRuleIdentity =
    Boolean(originalIdentity) &&
    getRuleIdentityKey(originalIdentity as RuleIdentity) !== getRuleIdentityKey(submitRule);

  isSubmittingRule.value = true;
  try {
    let res;
    if (editingRuleId.value && !isReplacingRuleIdentity) {
      res = await axios.put(
        buildBackendUrl(`/api/custom-rules/${editingRuleId.value}`),
        submitData
      );
    } else {
      res = await axios.post(
        buildBackendUrl("/api/custom-rules"),
        submitData
      );
    }
    if (res.data.code === 200) {
      if (isReplacingRuleIdentity && originalIdentity) {
        await persistRuleDeletions([
          {
            type: originalIdentity.type,
            payload: originalIdentity.payload,
            target: submitRule.target,
          },
        ]);
      }
      ElMessage.success(
        isReplacingRuleIdentity
          ? "规则已替换并同步删除旧规则！"
          : editingRuleId.value
            ? "规则更新成功！"
            : "规则已云端接管生效！"
      );
      ruleDialogVisible.value = false;
      editingOriginalRuleIdentity.value = null;
      await fetchCustomData();
      if (activeProfileId.value) {
        await loadSubscription({ preferredTab: "rules" });
      }
    } else {
      throw new Error(res.data.message);
    }
  } catch (error: any) {
    ElMessage.error(
      "保存失败: " + (error.response?.data?.message || error.message)
    );
  } finally {
    isSubmittingRule.value = false;
  }
};

const isCustomRule = (row: any) => {
  const customInfo = customRulesDict.value[getRuleIdentityKey(row)];
  return !!customInfo && customInfo.Target !== deletedCustomRuleTarget;
};

// ---------------------- 个人中心 (代理设置、修改密码与退出登录) ----------------------
interface SubscriptionFetchSettings {
  proxy_enabled: boolean;
  proxy_pool: string;
}

const proxySettingsVisible = ref(false);
const isLoadingProxySettings = ref(false);
const isSavingProxySettings = ref(false);
const proxySettingsForm = ref<SubscriptionFetchSettings>({
  proxy_enabled: false,
  proxy_pool: "",
});
let proxySettingsLoadRequestId = 0;

const resetProxySettingsForm = () => {
  proxySettingsForm.value = {
    proxy_enabled: false,
    proxy_pool: "",
  };
};

const clearProxySettingsState = () => {
  proxySettingsLoadRequestId += 1;
  isLoadingProxySettings.value = false;
  isSavingProxySettings.value = false;
  resetProxySettingsForm();
};

const openProxySettings = async () => {
  const requestId = ++proxySettingsLoadRequestId;
  resetProxySettingsForm();
  proxySettingsVisible.value = true;
  isLoadingProxySettings.value = true;
  try {
    const res = await axios.get(buildBackendUrl("/api/subscription-fetch-settings"));
    if (requestId !== proxySettingsLoadRequestId) return;
    if (res.data.code !== 200) {
      throw new Error(res.data.message || "获取代理池设置失败");
    }
    const settings = res.data.data || {};
    proxySettingsForm.value = {
      proxy_enabled: Boolean(settings.proxy_enabled),
      proxy_pool: typeof settings.proxy_pool === "string" ? settings.proxy_pool : "",
    };
  } catch (error: any) {
    if (requestId !== proxySettingsLoadRequestId) return;
    proxySettingsVisible.value = false;
    ElMessage.error(error.response?.data?.message || error.message || "获取代理池设置失败");
  } finally {
    if (requestId === proxySettingsLoadRequestId) {
      isLoadingProxySettings.value = false;
    }
  }
};

const saveProxySettings = async () => {
  const proxyPool = proxySettingsForm.value.proxy_pool.trim();
  if (proxySettingsForm.value.proxy_enabled && !proxyPool) {
    ElMessage.warning("开启代理抓取后，请至少填写一个代理");
    return;
  }

  isSavingProxySettings.value = true;
  try {
    const payload: SubscriptionFetchSettings = {
      proxy_enabled: proxySettingsForm.value.proxy_enabled,
      proxy_pool: proxyPool,
    };
    const res = await axios.put(buildBackendUrl("/api/subscription-fetch-settings"), payload);
    if (res.data.code !== 200) {
      throw new Error(res.data.message || "保存代理池设置失败");
    }
    proxySettingsForm.value = payload;
    proxySettingsVisible.value = false;
    ElMessage.success("代理池设置已保存");
  } catch (error: any) {
    ElMessage.error(error.response?.data?.message || error.message || "保存代理池设置失败");
  } finally {
    isSavingProxySettings.value = false;
  }
};

const changePasswordVisible = ref(false);
const isChangingPassword = ref(false);
const passwordFormRef = ref<any>(null);

const passwordForm = ref({
  oldPassword: "",
  newPassword: "",
  confirmPassword: "",
});

const validateConfirmPassword = (_rule: any, value: string, callback: any) => {
  if (value === "") {
    callback(new Error("请再次输入新密码"));
  } else if (value !== passwordForm.value.newPassword) {
    callback(new Error("两次输入密码不一致!"));
  } else {
    callback();
  }
};

const passwordRules = {
  oldPassword: [
    { required: true, message: "请输入当前密码", trigger: "blur" },
    { min: 5, message: "密码长度不能小于 5 位", trigger: "blur" },
  ],
  newPassword: [
    { required: true, message: "请输入新密码", trigger: "blur" },
    { min: 5, message: "密码长度不能小于 5 位", trigger: "blur" },
  ],
  confirmPassword: [
    { required: true, validator: validateConfirmPassword, trigger: "blur" },
  ],
};

const fileInputRef = ref<HTMLInputElement | null>(null);

const handleFileUpload = async (event: any) => {
  const file = event.target.files[0];
  if (!file) return;

  try {
    await ElMessageBox.confirm(
      "此操作将不可逆地覆盖当前所有的自定义节点、策略组与分流规则，是否确认导入？",
      "⚠️ 高危操作确认",
      {
        confirmButtonText: "确认覆盖并导入",
        cancelButtonText: "取消",
        type: "warning",
        customClass: "glass-dialog",
      }
    );

    const reader = new FileReader();
    reader.onload = async (e) => {
      try {
        const json = JSON.parse(e.target?.result as string);
        const res = await axios.post(buildBackendUrl("/api/import"), json);
        if (res.data.code === 200) {
          ElMessage.success(res.data.message || "备份导入成功！");
          await fetchCustomData();
        } else {
          ElMessage.error(res.data.message || "导入失败");
        }
      } catch (err: any) {
        ElMessage.error(
          "文件解析或请求失败: " +
            (err.response?.data?.message || err.message)
        );
      } finally {
        if (fileInputRef.value) fileInputRef.value.value = "";
      }
    };
    reader.readAsText(file);
  } catch {
    if (fileInputRef.value) fileInputRef.value.value = "";
  }
};

const handleUserCommand = async (command: string) => {
  if (command === "logout") {
    try {
      await ElMessageBox.confirm("确定要退出登录吗？", "提示", {
        confirmButtonText: "确定",
        cancelButtonText: "取消",
        type: "warning",
        customClass: "glass-dialog",
      });
      
      try {
        await axios.post(buildBackendUrl("/api/logout"));
      } catch (e) {
        console.error("Logout request failed", e);
      }
      
      localStorage.removeItem("token");
      clearProxySettingsState();
      proxySettingsVisible.value = false;
      isLoggedIn.value = false;
      ElMessage.success("已成功退出登录");
    } catch {
      // cancel
    }
  } else if (command === "changePassword") {
    passwordForm.value = {
      oldPassword: "",
      newPassword: "",
      confirmPassword: "",
    };
    changePasswordVisible.value = true;
    if (passwordFormRef.value) {
      passwordFormRef.value.resetFields();
    }
  } else if (command === "proxySettings") {
    await openProxySettings();
  } else if (command === "backupData") {
    try {
      const res = await axios.get(buildBackendUrl("/api/backup"), {
        responseType: "blob",
      });
      const url = window.URL.createObjectURL(new Blob([res.data]));
      const link = document.createElement("a");
      link.href = url;
      link.setAttribute("download", `clash_subscription_decoder_backup_${new Date().getTime()}.json`);
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
      ElMessage.success("数据备份已成功下载！");
    } catch (error: any) {
      ElMessage.error("数据备份失败");
    }
  } else if (command === "importData") {
    if (fileInputRef.value) {
      fileInputRef.value.click();
    }
  }
};

const handleMobileActionCommand = (command: string) => {
  if (command === "addNode") {
    openNodeDialog();
    return;
  }
  if (command === "addGroup") {
    openGroupDialog();
    return;
  }
  if (command === "addRule") {
    openRuleDialog();
    return;
  }
  if (command === "copyGroups") {
    openCopyGroupsDialog();
    return;
  }
  void handleUserCommand(command);
};

const submitChangePassword = async () => {
  if (!passwordFormRef.value) return;
  
  await passwordFormRef.value.validate(async (valid: boolean) => {
    if (valid) {
      isChangingPassword.value = true;
      try {
        const res = await axios.post(buildBackendUrl("/api/change-password"), {
          old_password: passwordForm.value.oldPassword,
          new_password: passwordForm.value.newPassword,
        });
        
        if (res.data.code === 200) {
          ElMessage.success("密码修改成功，请重新登录！");
          changePasswordVisible.value = false;
          localStorage.removeItem("token");
          isLoggedIn.value = false;
        } else {
          ElMessage.error(res.data.message || "修改失败");
        }
      } catch (error: any) {
        ElMessage.error(
          error.response?.data?.message || error.message || "请求失败，请检查网络"
        );
      } finally {
        isChangingPassword.value = false;
      }
    }
  });
};
</script>

<template>
  <svg
    class="iconfont-symbol-sprite"
    aria-hidden="true"
    focusable="false"
    width="0"
    height="0"
  >
    <defs>
      <linearGradient
        id="icon-shadowrocket-gradient"
        x1="21"
        y1="6"
        x2="43"
        y2="62"
        gradientUnits="userSpaceOnUse"
      >
        <stop offset="0" stop-color="#4fc3f7" />
        <stop offset="0.55" stop-color="#7c72ea" />
        <stop offset="1" stop-color="#9b5de5" />
      </linearGradient>
    </defs>
    <symbol id="icon-shadowrocket-logo" viewBox="0 0 64 64">
      <path
        fill="none"
        stroke="url(#icon-shadowrocket-gradient)"
        stroke-width="5.2"
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M32 6.5C22.2 13.8 17.6 26.7 19.2 41.1L10.4 43.8C11.2 35.2 14.1 29.5 19 26.6M45 26.6C49.9 29.5 52.8 35.2 53.6 43.8L44.8 41.1C46.4 26.7 41.8 13.8 32 6.5Z"
      />
      <path
        fill="none"
        stroke="url(#icon-shadowrocket-gradient)"
        stroke-width="5.2"
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M24.8 41.8C26.8 48.2 37.2 48.2 39.2 41.8"
      />
      <circle
        cx="32"
        cy="25.2"
        r="6.1"
        fill="none"
        stroke="url(#icon-shadowrocket-gradient)"
        stroke-width="5.2"
      />
      <path
        fill="none"
        stroke="url(#icon-shadowrocket-gradient)"
        stroke-width="5.2"
        stroke-linecap="round"
        stroke-linejoin="round"
        d="M32 50.6C27.3 55.5 29.1 60 32 62.2C34.9 60 36.7 55.5 32 50.6Z"
      />
    </symbol>
  </svg>
  <div v-if="isAppBootstrapping" class="app-bootstrap">
    <div class="bootstrap-shell">
      <header class="bootstrap-header glass-card">
        <div class="bootstrap-brand">
          <span class="bootstrap-logo">⚡</span>
          <div>
            <div class="bootstrap-brand-title">CLASH SUBSCRIPTION DECODER</div>
            <div class="bootstrap-brand-subtitle">安全控制台初始化中</div>
          </div>
        </div>
        <div class="bootstrap-status">
          <span class="pulse-dot"></span>
          数据加载中
        </div>
      </header>

      <section class="bootstrap-panel glass-card">
        <div class="bootstrap-copy">
          <el-icon class="is-loading bootstrap-icon"><Loading /></el-icon>
          <div>
            <h2>{{ appBootstrapTitle }}</h2>
            <p>{{ appBootstrapDesc }}</p>
          </div>
        </div>
        <el-skeleton :rows="4" animated />
      </section>

      <section class="bootstrap-grid">
        <div v-for="item in 3" :key="item" class="bootstrap-card glass-card">
          <el-skeleton :rows="5" animated />
        </div>
      </section>
    </div>
  </div>
  <Login v-else-if="!isLoggedIn" @login-success="onLoginSuccess" />
  <div v-else class="app-container">
    <!-- 头部精致毛玻璃导航栏 -->
    <header class="main-header glass-card">
      <div class="logo-wrapper">
        <span class="logo-icon">⚡</span>
        <h1 class="logo-title text-gradient">CLASH SUBSCRIPTION DECODER</h1>
      </div>
      <div class="header-actions">
        <el-tag
          size="large"
          type="primary"
          effect="dark"
          round
          class="status-indicator"
          style="margin-right: 16px;"
        >
          <span class="pulse-dot"></span>后端就绪 (:8080)
        </el-tag>

        <el-dropdown trigger="click" @command="handleUserCommand">
          <span class="el-dropdown-link user-dropdown">
            <el-avatar :size="28" class="user-avatar">A</el-avatar>
            <span class="username-text">管理员</span>
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="backupData" :icon="Download">备份数据</el-dropdown-item>
              <el-dropdown-item command="importData" :icon="Upload">导入备份</el-dropdown-item>
              <el-dropdown-item divided command="proxySettings" :icon="Setting">代理池设置</el-dropdown-item>
              <el-dropdown-item command="changePassword" :icon="Lock">修改密码</el-dropdown-item>
              <el-dropdown-item divided command="logout" :icon="SwitchButton">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </header>

    <!-- 隐藏的导入文件上传组件 -->
    <input 
      type="file" 
      ref="fileInputRef" 
      accept=".json" 
      style="display: none" 
      @change="handleFileUpload" 
    />

	    <!-- 中部内容主体区 -->
	    <main class="main-content">
	      <!-- 多配置管理面板 -->
	      <section class="profiles-panel glass-card" v-loading="isProfilesLoading">
	        <div class="profiles-header">
	          <div>
	            <h2 class="section-title">配置管理</h2>
	            <p class="section-desc">
	              当前订阅链接、节点、策略组和规则都跟随所选配置独立生效。
	            </p>
	          </div>
	          <div class="profiles-actions icon-action-row" aria-label="配置快捷操作">
	            <IconTooltipButton
	              label="新增配置"
	              type="primary"
	              :icon="Plus"
	              @click="openCreateProfileDialog"
	            />
	            <IconTooltipButton
	              label="刷新当前配置"
	              :disabled="!currentProfile"
	              :icon="Refresh"
	              :loading="isLoading"
	              @click="refreshCurrentProfile"
	            />
	            <IconTooltipButton
	              label="从其他配置复制代理组"
	              type="warning"
	              plain
	              :disabled="copyGroupSourceOptions.length === 0"
	              :icon="CopyDocument"
	              @click="openCopyGroupsDialog"
	            />
	          </div>
	          <div v-if="currentProfile?.source_type === 'local'" class="manual-config-actions icon-action-row" aria-label="本地配置新增操作">
	            <IconTooltipButton label="添加节点" type="primary" plain :icon="Plus" @click="openNodeDialog" />
	            <IconTooltipButton label="添加代理组" type="success" plain :icon="Plus" @click="openGroupDialog" />
	            <IconTooltipButton label="添加规则" type="warning" plain :icon="Plus" @click="openRuleDialog" />
	          </div>
	        </div>

	        <div v-if="profiles.length > 0" class="profiles-grid">
	          <button
	            v-for="profile in profiles"
	            :key="profile.id"
	            type="button"
	            class="profile-card"
	            :class="{ 'is-active': activeProfileId === profile.id }"
	            @click="selectProfile(profile)"
	          >
	            <div class="profile-card-main">
	              <span class="profile-name">{{ profile.name }}</span>
	              <el-tag
	                size="small"
	                :type="profile.source_type === 'local' ? 'success' : profileSubscriptionCount(profile) > 1 ? 'warning' : 'primary'"
	              >
	                {{
	                  profile.source_type === 'local'
	                    ? '本地手动'
	                    : profileSubscriptionCount(profile) > 1
	                      ? `多订阅 ${profileSubscriptionCount(profile)}`
	                      : '远程订阅'
	                }}
	              </el-tag>
	            </div>
	            <div v-if="profile.source_type === 'local'" class="profile-meta">
	              不依赖订阅地址
	            </div>
	            <div v-else class="profile-source-summary">
	              <div
	                v-for="source in profileDisplaySources(profile)"
	                :key="`${profile.id}-${source.url}`"
	                class="profile-source-chip"
	                :class="{ 'is-primary': source.is_primary }"
	                :title="source.url"
	              >
	                <span class="profile-source-role">{{ source.is_primary ? '主订阅' : '附加' }}</span>
	                <span class="profile-source-url">{{ source.url }}</span>
	              </div>
	            </div>
	            <div class="profile-actions" @click.stop>
	              <IconTooltipButton
	                label="编辑配置"
	                size="small"
	                text
	                type="primary"
	                :icon="Edit"
	                @click="openEditProfileDialog(profile)"
	              />
	              <IconTooltipButton
	                label="删除配置"
	                size="small"
	                text
	                type="danger"
	                :icon="Delete"
	                @click="deleteProfile(profile)"
	              />
	            </div>
	          </button>
	        </div>
	        <el-empty v-else description="暂无配置，请先新增一个远程订阅或本地手动配置" />
	      </section>

	      <!-- 控制卡片面板 -->
	      <section class="control-panel glass-card">
	        <h2 class="section-title">
	          {{ currentProfile?.source_type === 'local' ? '本地手动配置预览' : 'Clash Subscription Decoder' }}
	        </h2>
	        <p class="section-desc">
	          <template v-if="currentProfile?.source_type === 'local'">
	            本地配置不请求上游订阅，系统会根据你手动添加的节点、代理组和规则自动生成 YAML。
	          </template>
	          <template v-else>
	            输入任意提供 Base64 编码数据的订阅地址或接口
	            URL，后端将自动请求、清洗并进行多重自适应解码。
	          </template>
	        </p>

	        <div class="input-area">
	          <el-input
	            v-model="inputUrl"
	            placeholder="请输入订阅地址 URL (例如: https://example.com/sub)..."
	            clearable
	            @clear="handleClear"
	            @keyup.enter="handleDecode"
	            :disabled="isLoading || currentProfile?.source_type === 'local' || isMultiSubscriptionProfile"
	            class="decode-input"
	          >
            <template #prefix>
              <el-icon class="input-prefix-icon"><Link /></el-icon>
            </template>
          </el-input>

	          <div
	            v-if="currentProfile?.source_type === 'remote' && isMultiSubscriptionProfile"
	            class="active-source-strip"
	          >
	            <div
	              v-for="source in profileDisplaySources(currentProfile)"
	              :key="source.url"
	              class="active-source-item"
	              :class="{ 'is-primary': source.is_primary }"
	              :title="source.url"
	            >
	              <span>{{ source.is_primary ? '主订阅' : '附加订阅' }}</span>
	              <strong>{{ source.url }}</strong>
	            </div>
	          </div>

          <div class="button-group icon-action-row" aria-label="订阅处理操作">
	            <IconTooltipButton
	              v-if="hasSubscription"
	              :label="currentProfile?.source_type === 'local' ? '生成本地订阅' : '刷新订阅'"
	              type="success"
	              :icon="Refresh"
	              :loading="isLoading"
	              @click="currentProfile?.source_type === 'local' ? refreshCurrentProfile() : handleDecode()"
	            />
	            <IconTooltipButton
	              v-else
	              :label="currentProfile?.source_type === 'local' ? '生成本地预览' : '抓取并解码'"
	              type="primary"
	              :icon="Download"
	              :loading="isLoading"
	              @click="currentProfile?.source_type === 'local' ? refreshCurrentProfile() : handleDecode()"
	            />
	            <IconTooltipButton
	              label="Mock 快速测试"
	              type="info"
	              plain
	              :icon="Link"
	              :disabled="isLoading || currentProfile?.source_type === 'local' || isMultiSubscriptionProfile"
	              @click="handleQuickMock"
	            />
          </div>
        </div>

        <!-- 错误消息提醒 -->
        <transition name="fade">
          <div v-if="errorMsg" class="error-alert">
            <el-alert
              :title="errorMsg"
              type="error"
              show-icon
              :closable="false"
            />
          </div>
        </transition>
      </section>

      <!-- 骨架屏加载动画 -->
      <section v-if="isLoading" class="skeleton-wrapper glass-card">
        <el-skeleton :rows="5" animated />
      </section>

      <!-- 结果展示板块 -->
      <transition name="slide-up">
        <section v-if="result" class="result-panel glass-card">
          <!-- 面板顶栏 -->
          <div class="result-header">
            <div class="meta-info">
              <div class="meta-item">
                <span class="meta-label">文件体积：</span>
                <el-tag size="small" effect="plain" type="info"
                  >{{ (stats.size / 1024).toFixed(2) }} KB</el-tag
                >
              </div>
              <div class="meta-item">
                <span class="meta-label">总行数：</span>
                <el-tag size="small" effect="plain" type="info"
                  >{{ stats.lines }} 行</el-tag
                >
              </div>
              <div v-if="parsedNodes.length > 0" class="meta-item">
                <span class="meta-label">检测到节点：</span>
                <el-tag size="small" effect="dark" type="success"
                  >{{ parsedNodes.length }} 个</el-tag
                >
              </div>
              <div v-if="proxyGroups.length > 0" class="meta-item">
                <span class="meta-label">代理组：</span>
                <el-tag size="small" effect="plain" type="primary"
                  >{{ proxyGroups.length }} 个</el-tag
                >
              </div>
              <div v-if="parsedRuleLines.length > 0" class="meta-item">
                <span class="meta-label">规则：</span>
                <el-tag size="small" effect="plain" type="warning"
                  >{{ parsedRuleLines.length }} 条</el-tag
                >
              </div>
              <div v-if="providerItems.length > 0" class="meta-item">
                <span class="meta-label">Provider：</span>
                <el-tag size="small" effect="plain" type="info"
                  >{{ providerItems.length }} 个</el-tag
                >
              </div>
            </div>

            <div class="result-actions">
              <div class="result-action-group icon-action-row" aria-label="结果操作">
                <IconTooltipButton
                  label="复制明文配置"
                  :icon="CopyDocument"
                  @click="handleCopy"
                />
                <IconTooltipButton
                  label="导出 YAML"
                  :icon="Download"
                  @click="handleDownload"
                />
                <IconTooltipButton
                  label="复制订阅地址"
                  type="warning"
                  :icon="Link"
                  :loading="isCopyingSubLink"
                  @click="copyCurrentSubLink"
                />
                <IconTooltipButton
                  label="重新生成订阅"
                  type="danger"
                  :icon="Refresh"
                  :loading="isGeneratingSubLink"
                  @click="regenerateSubLink"
                />
              </div>
            </div>
          </div>

          <!-- 页签切换区 -->
          <el-tabs v-model="activeTab" class="custom-tabs">
            <!-- 节点预览页签 -->
            <el-tab-pane name="nodes" v-if="draggableNodes.length > 0">
              <template #label>
                <span class="tab-label"
                  >⚡ 节点解析概览 ({{ draggableNodes.length }})</span
                >
              </template>
              <div
                class="nodes-header-actions"
                style="
                  display: flex;
                  justify-content: flex-end;
                  align-items: center;
                  gap: 10px;
                  margin-bottom: 16px;
                "
              >
                <el-tag v-if="isSavingNodeOrder" type="warning" effect="dark">
                  排序保存中...
                </el-tag>
                <IconTooltipButton
                  label="新增自定义节点"
                  type="success"
                  :icon="Plus"
                  @click="openNodeDialog"
                />
              </div>
              <div class="nodes-grid">
                <div
                  v-for="(node, idx) in draggableNodes"
                  :key="node.name || idx"
                  :class="[
                    'node-card',
                    {
                      'is-drag-over': isDragOverItem('nodes', idx),
                      'is-order-saving': isSavingNodeOrder,
                    },
                  ]"
                  @dragover="handleSortDragOver($event, 'nodes', idx)"
                  @drop="handleSortDrop($event, 'nodes', idx)"
                >
                  <div class="node-card-header">
                    <div
                      style="
                        display: flex;
                        align-items: center;
                        overflow: hidden;
                        flex: 1;
                      "
                    >
                      <span
                        class="drag-handle"
                        :class="{ disabled: isSavingNodeOrder }"
                        :draggable="!isSavingNodeOrder"
                        title="拖拽排序"
                        @dragstart="handleSortDragStart($event, 'nodes', idx)"
                        @dragend="handleSortDragEnd"
                      >
                        ☰
                      </span>
                      <span class="node-flag">{{
                        getFlagEmoji(node.name)
                      }}</span>
                      <span class="node-name" :title="node.name">{{
                        node.name
                      }}</span>
                    </div>
                    <div class="custom-actions" style="display: flex; gap: 4px">
                      <IconTooltipButton
                        :label="customNodesDict[node.name] ? '编辑自定义节点' : '编辑并接管订阅节点'"
                        type="primary"
                        link
                        size="small"
                        :icon="Edit"
                        @click="editCustomNode(node)"
                      />
                      <IconTooltipButton
                        :label="customNodesDict[node.name] ? '删除自定义节点' : '删除订阅节点'"
                        type="danger"
                        link
                        size="small"
                        :icon="Delete"
                        @click="deleteCustomNode(node)"
                      />
                    </div>
                  </div>
                  <div class="node-card-body">
                    <div class="node-info-row">
                      <span class="info-label">地址:</span>
                      <span class="info-val" :title="node.server">{{
                        node.server
                      }}</span>
                    </div>
                    <div class="node-info-row">
                      <span class="info-label">端口:</span>
                      <span class="info-val highlight-port">{{
                        node.port
                      }}</span>
                    </div>
                  </div>
                  <div class="node-card-footer">
                    <el-tag
                      size="small"
                      :type="getNodeTypeTag(node.type).type"
                      effect="dark"
                      class="node-type-tag"
                    >
                      {{ getNodeTypeTag(node.type).label }}
                    </el-tag>
                    <span v-if="node.details.cipher" class="cipher-label">{{
                      node.details.cipher
                    }}</span>
                  </div>
                </div>
              </div>
            </el-tab-pane>

            <!-- 代理组页签 -->
            <el-tab-pane name="groups" v-if="draggableGroups.length > 0">
              <template #label>
                <span class="tab-label"
                  >🗂️ 代理组策略 ({{ draggableGroups.length }})</span
                >
              </template>

              <div
                class="groups-header-actions"
                style="
                  display: flex;
                  justify-content: flex-end;
                  align-items: center;
                  gap: 10px;
                  margin-bottom: 16px;
                "
              >
                <el-tag v-if="isSavingGroupOrder" type="warning" effect="dark">
                  排序保存中...
                </el-tag>
                <IconTooltipButton
                  label="从其他配置复制代理组"
                  type="warning"
                  plain
                  :disabled="copyGroupSourceOptions.length === 0"
                  :icon="CopyDocument"
                  @click="openCopyGroupsDialog"
                />
                <IconTooltipButton
                  label="新增自定义策略组"
                  type="primary"
                  :icon="Plus"
                  @click="openGroupDialog"
                />
              </div>

              <div class="groups-grid">
                <div
                  v-for="(group, idx) in draggableGroups"
                  :key="group.name || idx"
                  :class="[
                    'group-card',
                    {
                      'is-drag-over': isDragOverItem('groups', idx),
                      'is-order-saving': isSavingGroupOrder,
                    },
                  ]"
                  @dragover="handleSortDragOver($event, 'groups', idx)"
                  @drop="handleSortDrop($event, 'groups', idx)"
                >
                  <div class="group-card-header">
                    <div class="group-card-main">
                      <span
                        class="drag-handle"
                        :class="{ disabled: isSavingGroupOrder }"
                        :draggable="!isSavingGroupOrder"
                        title="拖拽排序"
                        @dragstart="handleSortDragStart($event, 'groups', idx)"
                        @dragend="handleSortDragEnd"
                      >
                        ☰
                      </span>
                      <span class="group-name">{{ group.name }}</span>
                      <el-tag
                        size="small"
                        type="primary"
                        effect="dark"
                        class="group-type-tag"
                        >{{ group.type }}</el-tag
                      >
                      <span
                        v-if="isShadowrocketBuiltinProxyGroup(group)"
                        class="shadowrocket-badge"
                        role="img"
                        aria-label="Shadowrocket 映射为内置 PROXY"
                        title="Shadowrocket 映射为内置 PROXY"
                      >
                        <svg class="iconfont iconfont-shadowrocket" aria-hidden="true">
                          <use href="#icon-shadowrocket-logo" />
                        </svg>
                      </span>
                    </div>
                    <div class="custom-actions" style="display: flex; gap: 4px">
                      <IconTooltipButton
                        :label="customGroupsDict[group.name] ? '编辑自定义策略组' : '编辑并接管订阅策略组'"
                        type="primary"
                        link
                        size="small"
                        :icon="Edit"
                        @click="editCustomGroup(group)"
                      />
                      <IconTooltipButton
                        :label="customGroupsDict[group.name] ? '删除自定义策略组' : '删除订阅策略组'"
                        type="danger"
                        link
                        size="small"
                        :icon="Delete"
                        @click="deleteCustomGroup(group)"
                      />
                    </div>
                  </div>
                  <div class="group-card-body">
                    <el-tag
                      v-for="proxy in group.proxies"
                      :key="proxy"
                      size="small"
                      effect="plain"
                      type="info"
                      class="proxy-tag"
                      disable-transitions
                    >
                      {{ proxy }}
                    </el-tag>
                  </div>
                </div>
              </div>
            </el-tab-pane>

            <!-- 规则列表页签 -->
            <el-tab-pane name="rules" v-if="parsedRuleLines.length > 0">
              <template #label>
                <span class="tab-label"
                  >📋 分流规则 ({{ parsedRuleLines.length }})</span
                >
              </template>

              <div class="rules-container glass-card">
                <div class="rules-toolbar">
                  <div class="rules-toolbar-main">
                    <div class="rules-filter-group">
                      <el-select
                        v-model="ruleTargetFilter"
                        placeholder="目标策略过滤"
                        clearable
                        filterable
                        class="rule-target-filter"
                        popper-class="glass-dropdown"
                      >
                        <el-option label="[全部策略]" value="" />
                        <el-option v-for="t in ruleTargets" :key="t" :label="t" :value="t" />
                      </el-select>
                      <el-input
                        v-model="ruleSearchQuery"
                        placeholder="搜索规则类型、内容或目标策略"
                        clearable
                        class="rule-search-input"
                      >
                        <template #prefix>
                          <el-icon><Search /></el-icon>
                        </template>
                      </el-input>
                    </div>

                    <div class="rules-primary-actions">
                      <IconTooltipButton
                        v-if="hasDirtyRules"
                        :label="`应用修改（${dirtyRuleCount}）`"
                        type="success"
                        :icon="Select"
                        :loading="isSubmittingRule"
                        @click="batchSaveRules"
                      />
                      <IconTooltipButton
                        label="新增规则"
                        type="primary"
                        :icon="Plus"
                        @click="openRuleDialog"
                      />
                      <el-dropdown trigger="click" @command="handleRuleToolbarCommand">
                        <el-tooltip content="更多规则操作" placement="top" effect="dark" popper-class="app-tooltip">
                          <el-button
                            class="rules-more-button"
                            circle
                            :icon="MoreFilled"
                            aria-label="更多规则操作"
                          />
                        </el-tooltip>
                        <template #dropdown>
                          <el-dropdown-menu>
                            <el-dropdown-item
                              v-if="currentProfile?.source_type === 'remote'"
                              command="localizeRemoteRules"
                              :disabled="isLocalizingRules"
                            >
                              {{ isLocalizingRules ? "正在本地化远程规则" : "本地化远程规则" }}
                            </el-dropdown-item>
                            <el-dropdown-item
                              command="copyRules"
                              :disabled="copyRuleSourceOptions.length === 0 || isCopyingRules"
                            >
                              从其他配置复制规则
                            </el-dropdown-item>
                            <el-dropdown-item
                              divided
                              command="deleteFilteredRules"
                              :disabled="!canDeleteFilteredRules"
                              class="rules-danger-dropdown-item"
                            >
                              {{ isDeletingFilteredRules ? "正在删除筛选规则" : "删除当前目标策略规则" }}
                            </el-dropdown-item>
                          </el-dropdown-menu>
                        </template>
                      </el-dropdown>
                    </div>
                  </div>

                  <div class="rules-batch-panel">
                    <div class="rules-batch-copy">
                      <span class="rules-batch-title">批量调整</span>
                      <span class="rules-batch-desc">
                        作用于当前筛选出的 {{ parsedRules.length }} 条规则
                      </span>
                    </div>
                    <div class="rules-batch-controls">
                      <el-select
                        v-model="bulkRuleTarget"
                        placeholder="批量设置为目标策略"
                        clearable
                        filterable
                        allow-create
                        default-first-option
                        class="rule-bulk-target-select"
                        popper-class="glass-dropdown"
                      >
                        <el-option
                          v-for="option in builtInRuleTargetOptions"
                          :key="option.value"
                          :label="option.label"
                          :value="option.value"
                        />
                        <el-option-group label="现有策略组">
                          <el-option
                            v-for="g in proxyGroups"
                            :key="g.name"
                            :label="g.name"
                            :value="g.name"
                          />
                        </el-option-group>
                        <el-option-group label="现有独立节点">
                          <el-option
                            v-for="n in parsedNodes"
                            :key="n.name"
                            :label="n.name"
                            :value="n.name"
                          />
                        </el-option-group>
                      </el-select>
                      <IconTooltipButton
                        label="设置筛选结果"
                        type="primary"
                        plain
                        :disabled="!bulkRuleTarget || !hasFilteredRules"
                        :icon="Select"
                        @click="applyBulkRuleTargetToFilteredRules"
                      />
                    </div>
                  </div>
                </div>

                <el-table
                  :data="paginatedRules"
                  height="500"
                  class="custom-table"
                  style="width: 100%"
                >
                  <el-table-column prop="type" label="规则类型" width="160">
                    <template #default="scope">
                      <el-tag
                        size="small"
                        effect="dark"
                        type="warning"
                        class="rule-type-tag"
                        >{{ scope.row.type }}</el-tag
                      >
                    </template>
                  </el-table-column>
                  <el-table-column
                    prop="payload"
                    label="匹配内容"
                    show-overflow-tooltip
                  >
                    <template #default="scope">
                      <span class="rule-payload">{{ scope.row.payload }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column prop="target" label="目标策略" width="280">
                    <template #default="scope">
                      <div style="display: flex; align-items: center; gap: 8px;">
                        <el-select
                          v-model="scope.row.target"
                          size="small"
                          filterable
                          allow-create
                          default-first-option
                          style="width: 170px"
                          popper-class="glass-dropdown"
                          @change="markRuleDirty(scope.row)"
                        >
                          <el-option
                            v-for="option in builtInRuleTargetOptions"
                            :key="option.value"
                            :label="option.label"
                            :value="option.value"
                          />
                          <el-option-group label="现有策略组">
                            <el-option
                              v-for="g in proxyGroups"
                              :key="g.name"
                              :label="g.name"
                              :value="g.name"
                            />
                          </el-option-group>
                          <el-option-group label="现有独立节点">
                            <el-option
                              v-for="n in parsedNodes"
                              :key="n.name"
                              :label="n.name"
                              :value="n.name"
                            />
                          </el-option-group>
                        </el-select>
                        <el-tag
                          v-if="isCustomRule(scope.row)"
                          size="small"
                          type="danger"
                          effect="dark"
                        >已覆盖 (云端)</el-tag>
                      </div>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="150" fixed="right">
                    <template #default="scope">
                      <div style="display: flex; gap: 4px">
                        <IconTooltipButton
                          :label="isCustomRule(scope.row) ? '高级编辑' : '高级编辑并接管'"
                          type="primary"
                          link
                          size="small"
                          :icon="Edit"
                          @click="editRule(scope.row)"
                        />
                        <IconTooltipButton
                          label="删除规则"
                          type="danger"
                          link
                          size="small"
                          :icon="Delete"
                          @click="deleteRule(scope.row)"
                        />
                        <IconTooltipButton
                          v-if="isCustomRule(scope.row)"
                          label="移除接管（恢复原生）"
                          type="warning"
                          link
                          size="small"
                          :icon="Refresh"
                          @click="removeCustomRuleOverride(scope.row)"
                        />
                      </div>
                    </template>
                  </el-table-column>
                </el-table>

                <!-- 分页器 -->
                <div class="pagination-wrapper">
                  <el-pagination
                    v-model:current-page="currentRulePage"
                    v-model:page-size="rulePageSize"
                    :page-sizes="[50, 100, 200, 500]"
                    background
                    layout="total, sizes, prev, pager, next, jumper"
                    :total="parsedRules.length"
                  />
                </div>
              </div>
            </el-tab-pane>

            <!-- Provider 资源页签 -->
            <el-tab-pane name="providers" v-if="providerItems.length > 0">
              <template #label>
                <span class="tab-label">📦 Provider 资源 ({{ providerItems.length }})</span>
              </template>
              <div class="provider-panel">
                <div class="provider-summary">
                  <el-tag type="primary" effect="plain">
                    代理 Provider：{{ proxyProviderItems.length }}
                  </el-tag>
                  <el-tag type="warning" effect="plain">
                    规则 Provider：{{ ruleProviderItems.length }}
                  </el-tag>
                </div>
                <div class="provider-grid">
                  <div
                    v-for="provider in providerItems"
                    :key="`${provider.kind}-${provider.name}`"
                    class="provider-card"
                  >
                    <div class="provider-card-header">
                      <div class="provider-title">
                        <span>{{ provider.name }}</span>
                        <small>{{ provider.kind === 'proxy' ? '代理提供者' : '规则提供者' }}</small>
                      </div>
                      <el-tag
                        size="small"
                        :type="provider.kind === 'proxy' ? 'primary' : 'warning'"
                        effect="dark"
                      >
                        {{ provider.type }}
                      </el-tag>
                    </div>
                    <div class="provider-detail-list">
                      <div class="provider-detail-row" v-if="provider.url !== '-'">
                        <span>URL</span>
                        <strong :title="provider.url">{{ provider.url }}</strong>
                      </div>
                      <div class="provider-detail-row" v-if="provider.path !== '-'">
                        <span>路径</span>
                        <strong :title="provider.path">{{ provider.path }}</strong>
                      </div>
                      <div class="provider-detail-row" v-if="provider.behavior !== '-'">
                        <span>行为</span>
                        <strong>{{ provider.behavior }}</strong>
                      </div>
                      <div class="provider-detail-row" v-if="provider.format !== '-'">
                        <span>格式</span>
                        <strong>{{ provider.format }}</strong>
                      </div>
                      <div class="provider-detail-row" v-if="provider.interval !== '-'">
                        <span>刷新间隔</span>
                        <strong>{{ provider.interval }}</strong>
                      </div>
                      <div class="provider-detail-row">
                        <span>引用</span>
                        <strong>{{ providerUsageLabel(provider) }}</strong>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </el-tab-pane>

            <!-- 解析诊断页签 -->
            <el-tab-pane name="diagnostics" v-if="parseDiagnostics.length > 0">
              <template #label>
                <span class="tab-label">🧭 解析诊断 ({{ parseDiagnostics.length }})</span>
              </template>
              <div class="diagnostics-panel">
                <div class="diagnostics-metrics">
                  <el-tag effect="plain" type="success">节点：{{ parsedNodes.length }}</el-tag>
                  <el-tag effect="plain" type="primary">代理组：{{ proxyGroups.length }}</el-tag>
                  <el-tag effect="plain" type="warning">规则：{{ parsedRuleLines.length }}</el-tag>
                  <el-tag effect="plain" type="info">Provider：{{ providerItems.length }}</el-tag>
                </div>
                <el-alert
                  v-for="diagnostic in parseDiagnostics"
                  :key="diagnostic.title"
                  :type="diagnostic.type"
                  :title="diagnostic.title"
                  :description="diagnostic.description"
                  show-icon
                  :closable="false"
                />
              </div>
            </el-tab-pane>

            <!-- 明文文本页签 -->
            <el-tab-pane name="text">
              <template #label>
                <span class="tab-label">📄 解码后明文配置</span>
              </template>
              <div class="editor-glass-wrapper">
                <codemirror
                  v-model="result.decoded"
                  :extensions="cmExtensions"
                  :style="{
                    minHeight: '400px',
                    maxHeight: '600px',
                    fontSize: '14px',
                    borderRadius: '12px',
                  }"
                  :indent-with-tab="true"
                  :tab-size="2"
                />
              </div>
            </el-tab-pane>

            <!-- 原始响应数据页签 -->
            <el-tab-pane name="raw">
              <template #label>
                <span class="tab-label">🔗 原始响应截断</span>
              </template>
              <div class="code-wrapper">
                <div class="code-container raw-base64-text">
                  {{ result.raw_response }}
                </div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </section>
      </transition>
	    </main>

      <nav class="mobile-action-bar glass-card" aria-label="移动端快捷操作">
        <IconTooltipButton
          label="新增配置"
          type="primary"
          :icon="Plus"
          @click="openCreateProfileDialog"
        />
        <IconTooltipButton
          :label="hasSubscription ? '刷新当前订阅' : '抓取并解码'"
          type="success"
          :disabled="!currentProfile"
          :icon="Refresh"
          :loading="isLoading"
          @click="currentProfile?.source_type === 'local' ? refreshCurrentProfile() : handleDecode()"
        />
        <IconTooltipButton
          label="复制订阅地址"
          type="warning"
          :disabled="!activeProfileId"
          :icon="Link"
          :loading="isCopyingSubLink"
          @click="copyCurrentSubLink"
        />
        <el-dropdown trigger="click" @command="handleMobileActionCommand">
          <el-tooltip content="更多操作" placement="top" effect="dark" popper-class="app-tooltip">
            <el-button
              class="mobile-more-button"
              circle
              :icon="MoreFilled"
              aria-label="更多操作"
            />
          </el-tooltip>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="addNode">添加节点</el-dropdown-item>
              <el-dropdown-item command="addGroup">添加代理组</el-dropdown-item>
              <el-dropdown-item command="addRule">添加规则</el-dropdown-item>
              <el-dropdown-item
                command="copyGroups"
                :disabled="copyGroupSourceOptions.length === 0"
              >
                复制代理组
              </el-dropdown-item>
              <el-dropdown-item divided command="backupData">备份数据</el-dropdown-item>
              <el-dropdown-item command="importData">导入备份</el-dropdown-item>
              <el-dropdown-item divided command="proxySettings">代理池设置</el-dropdown-item>
              <el-dropdown-item command="changePassword">修改密码</el-dropdown-item>
              <el-dropdown-item divided command="logout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </nav>

	    <!-- 配置新增/编辑弹窗 -->
	    <el-dialog
	      v-model="profileDialogVisible"
	      :title="editingProfileId ? '编辑配置' : '新增配置'"
	      width="720px"
	      class="glass-dialog"
	    >
	      <el-form label-position="top">
	        <el-form-item label="配置名称">
	          <el-input v-model="profileForm.name" placeholder="例如：家庭路由、本地备用、公司网络" />
	        </el-form-item>
	        <el-form-item label="配置来源">
	          <el-segmented
	            v-model="profileForm.source_type"
	            :options="[
	              { label: '远程订阅', value: 'remote' },
	              { label: '本地手动', value: 'local' },
	            ]"
	          />
	        </el-form-item>
	        <el-form-item v-if="profileForm.source_type === 'remote'" label="远程订阅地址">
	          <div class="profile-source-list">
	            <div
	              v-for="(source, index) in profileForm.sources"
	              :key="index"
	              class="profile-source-row"
	            >
	              <el-input
	                v-model="source.url"
	                :placeholder="index === 0 ? 'https://example.com/sub' : 'https://example.com/backup-sub'"
	                clearable
	              />
	              <el-button
	                class="source-primary-btn"
	                :type="source.is_primary ? 'success' : 'primary'"
	                :plain="!source.is_primary"
	                @click="setPrimaryProfileSource(index)"
	              >
	                {{ source.is_primary ? '主订阅' : '设为主订阅' }}
	              </el-button>
	              <IconTooltipButton
	                label="删除订阅地址"
	                type="danger"
	                plain
	                :disabled="profileForm.sources.length <= 1"
	                :icon="Delete"
	                @click="removeProfileSource(index)"
	              />
	            </div>
	            <div class="profile-source-footer">
	              <el-button type="primary" plain :icon="Plus" @click="addProfileSource">
	                添加订阅地址
	              </el-button>
	              <span class="profile-source-tip">主订阅用于保留代理组和规则，其他订阅只合并节点。</span>
	            </div>
	          </div>
	        </el-form-item>
	        <el-form-item v-else label="本地手动配置说明">
	          <el-alert
	            type="success"
	            show-icon
	            :closable="false"
	            title="创建后请在当前配置下手动添加节点、代理组和规则；系统会自动生成 Clash/Mihomo YAML 订阅。"
	          />
	        </el-form-item>
	      </el-form>
	      <template #footer>
	        <el-button @click="profileDialogVisible = false" plain>取消</el-button>
	        <el-button type="primary" :loading="isSubmittingProfile" @click="saveProfile">
	          保存配置
	        </el-button>
	      </template>
	    </el-dialog>

	    <!-- 代理组复制弹窗 -->
	    <el-dialog
	      v-model="copyGroupsDialogVisible"
	      title="从其他配置复制代理组"
	      width="540px"
	      class="glass-dialog"
	    >
	      <el-form label-position="top">
	        <el-form-item label="来源配置">
	          <el-select v-model="copyGroupsSourceProfileId" style="width: 100%" popper-class="glass-dropdown">
	            <el-option
	              v-for="profile in copyGroupSourceOptions"
	              :key="profile.id"
	              :label="profile.name"
	              :value="profile.id"
	            />
	          </el-select>
	        </el-form-item>
	        <el-alert
	          type="warning"
	          show-icon
	          :closable="false"
	          title="复制后会清空当前配置的自定义代理组，最终代理组只保留来源配置的代理组结构。来源节点会替换为当前配置全部节点，分流规则不会自动复制。"
	        />
	      </el-form>
	      <template #footer>
	        <el-button @click="copyGroupsDialogVisible = false" plain>取消</el-button>
	        <el-button type="warning" :loading="isCopyingGroups" @click="copyGroupsFromProfile">
	          确认复制
	        </el-button>
	      </template>
	    </el-dialog>

	    <!-- 规则复制弹窗 -->
	    <el-dialog
	      v-model="copyRulesDialogVisible"
	      title="从其他配置复制规则"
	      width="520px"
	      class="glass-dialog"
	    >
	      <el-form label-position="top">
	        <el-form-item label="来源配置">
	          <el-select v-model="copyRulesSourceProfileId" style="width: 100%" popper-class="glass-dropdown">
	            <el-option
	              v-for="profile in copyRuleSourceOptions"
	              :key="profile.id"
	              :label="profile.name"
	              :value="profile.id"
	            />
	          </el-select>
	        </el-form-item>
	        <el-alert
	          type="warning"
	          show-icon
	          :closable="false"
	          title="复制后同类型、同匹配内容的规则会由来源配置覆盖，当前配置其他规则会保留。"
	        />
	      </el-form>
	      <template #footer>
	        <el-button @click="copyRulesDialogVisible = false" plain>取消</el-button>
	        <el-button type="warning" :loading="isCopyingRules" @click="copyRulesFromProfile">
	          确认复制
	        </el-button>
	      </template>
	    </el-dialog>

	    <!-- 自定义策略组弹窗 -->
	    <el-dialog
      v-model="groupDialogVisible"
      :title="
        editingGroupId ? '✨ 编辑云端自定义策略组' : '✨ 新增云端自定义策略组'
      "
      width="550px"
      class="glass-dialog"
    >
      <el-form label-position="top">
        <el-form-item label="策略组名称">
          <el-input
            v-model="newGroupForm.name"
            placeholder="例如：我的超强备用线路"
          ></el-input>
        </el-form-item>
        <el-form-item label="策略类型 (Type)">
          <el-select v-model="newGroupForm.type" style="width: 100%">
            <el-option
              v-for="t in groupTypes"
              :key="t"
              :label="t.toUpperCase()"
              :value="t"
            />
          </el-select>
        </el-form-item>
        <el-form-item>
          <template #label>
            <span class="form-label-with-tooltip">
              Shadowrocket 映射为内置 PROXY
              <el-tooltip
                content="开启后仅影响 Shadowrocket 配置：引用此代理组的规则和代理组成员会输出为内置 PROXY，跟随 Shadowrocket 首页当前选中的节点；Clash/Mihomo 与 Surge 不受影响。"
                placement="top"
                effect="dark"
                popper-class="app-tooltip"
              >
                <el-icon class="form-label-help"><InfoFilled /></el-icon>
              </el-tooltip>
            </span>
          </template>
          <el-switch
            v-model="newGroupForm.shadowrocket_use_builtin_proxy"
            active-text="开启"
            inactive-text="关闭"
          />
        </el-form-item>
        <el-form-item label="配置包含的目标代理">
          <div class="dialog-icon-actions" aria-label="策略组快捷填充">
            <IconTooltipButton
              label="注入全部最新节点 [ALL_NODES]"
              size="small"
              type="primary"
              plain
              :icon="Promotion"
              @click="selectAllNodes"
            />
            <IconTooltipButton
              label="引入所有现有策略组"
              size="small"
              type="info"
              plain
              :icon="CopyDocument"
              @click="selectAllExistingGroups"
            />
            <IconTooltipButton
              label="加入 DIRECT 直连"
              size="small"
              type="success"
              plain
              :icon="Connection"
              @click="selectDirectPolicy"
            />
            <IconTooltipButton
              label="加入 REJECT 拒绝"
              size="small"
              type="danger"
              plain
              :icon="CircleClose"
              @click="selectRejectPolicy"
            />
            <IconTooltipButton
              label="清空包含的代理"
              size="small"
              type="warning"
              plain
              :disabled="newGroupForm.proxies.length === 0"
              :icon="Remove"
              @click="clearGroupProxies"
            />
          </div>
          <el-select
            v-model="newGroupForm.proxies"
            multiple
            filterable
            placeholder="请选择需要包含的代理..."
            style="width: 100%"
            popper-class="glass-dropdown"
          >
            <el-option
              label="🌟 动态注入当前订阅全节点 [ALL_NODES]"
              value="[ALL_NODES]"
            />
            <el-option-group label="内置策略">
              <el-option
                v-for="item in builtInGroupProxies"
                :key="item.value"
                :label="item.label"
                :value="item.value"
              />
            </el-option-group>
            <el-option-group label="现有策略组">
              <el-option
                v-for="g in proxyGroups"
                :key="g.name"
                :label="g.name"
                :value="g.name"
              />
            </el-option-group>
            <el-option-group label="现有独立节点">
              <el-option
                v-for="n in parsedNodes"
                :key="n.name"
                :label="n.name"
                :value="n.name"
              />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item label="排除节点关键字或正则 (Exclude) - 极简可选">
          <el-input
            v-model="newGroupForm.exclude"
            placeholder="例如：特殊专线"
            clearable
          ></el-input>
          <p
            style="
              font-size: 12px;
              color: var(--text-secondary);
              margin-top: 4px;
              line-height: 1.4;
            "
          >
            仅当您有特殊的跨组排除需求时（如特定节点总是断流），才在此处填入正则表达式。
          </p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="groupDialogVisible = false" plain>取消</el-button>
        <el-button
          type="primary"
          @click="saveCustomGroup"
          :loading="isSubmittingGroup"
        >
          {{ editingGroupId ? "更新并立即云端同步" : "保存并立即云端同步" }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 自定义节点弹窗 -->
    <el-dialog
      v-model="nodeDialogVisible"
      :title="editingNodeId ? '✨ 编辑云端自定义节点' : '✨ 新增云端自定义节点'"
      width="600px"
      class="glass-dialog"
    >
      <el-tabs v-model="nodeActiveTab" class="custom-tabs node-dialog-tabs">
        <el-tab-pane label="🔗 链接智能导入" name="link">
          <div style="padding: 10px 0">
            <p
              style="
                color: var(--text-secondary);
                margin-bottom: 15px;
                font-size: 14px;
              "
            >
              支持自动解析 vless://, hysteria2://, ss://, socks5://
              等分享链接，一键提取核心参数。
            </p>
            <el-input
              v-model="nodeLinkForm.link"
              type="textarea"
              :rows="4"
              placeholder="请粘贴您的节点链接..."
            ></el-input>
            <div class="dialog-icon-actions dialog-icon-actions--end">
              <IconTooltipButton
                label="一键解析链接"
                type="primary"
                :icon="Download"
                :loading="isParsingLink"
                @click="parseNodeLink"
              />
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="✍️ 手动配置调整" name="manual">
          <el-form
            label-position="left"
            label-width="80px"
            style="padding: 10px 0; max-height: 400px; overflow-y: auto"
          >
            <el-form-item label="节点名称">
              <el-input
                v-model="newNodeForm.name"
                placeholder="请输入节点显示的名称"
              ></el-input>
            </el-form-item>
            <el-form-item label="协议类型">
              <el-select v-model="newNodeForm.type" style="width: 100%">
                <el-option
                  v-for="t in nodeTypes"
                  :key="t"
                  :label="t.toUpperCase()"
                  :value="t"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="服务器">
              <el-input
                v-model="newNodeForm.server"
                placeholder="例如：example.com 或 IP"
              ></el-input>
            </el-form-item>
            <el-form-item label="端口">
              <el-input-number
                v-model="newNodeForm.port"
                :min="1"
                :max="65535"
                style="width: 100%"
              ></el-input-number>
            </el-form-item>
            <el-form-item label="UDP 转发">
              <el-switch
                v-model="newNodeForm.config['udp']"
                active-text="开启"
                inactive-text="关闭"
              />
              <div
                style="
                  font-size: 12px;
                  color: var(--text-secondary);
                  margin-left: 15px;
                  display: inline-block;
                "
              >
                开启以支持转发 STUN 及其他 UDP 协议包
              </div>
            </el-form-item>
            <el-form-item label="前置拨号 (dialer-proxy)">
              <el-select
                v-model="newNodeForm.config['dialer-proxy']"
                clearable
                filterable
                placeholder="（可选）选择前置代理名，留空则直连"
                style="width: 100%"
                popper-class="glass-dropdown"
              >
                <el-option-group label="现有策略组">
                  <el-option
                    v-for="g in proxyGroups"
                    :key="g.name"
                    :label="g.name"
                    :value="g.name"
                  />
                </el-option-group>
                <el-option-group label="现有独立节点">
                  <el-option
                    v-for="n in parsedNodes"
                    :key="n.name"
                    :label="n.name"
                    :value="n.name"
                  />
                </el-option-group>
              </el-select>
              <p
                style="
                  font-size: 12px;
                  color: var(--text-secondary);
                  margin-top: 4px;
                  line-height: 1.4;
                "
              >
                最新内核移除了 relay，链式代理现由前置拨号 (dialer-proxy)
                原生接管。
              </p>
            </el-form-item>
            <el-form-item label="详细配置">
              <p
                style="
                  font-size: 12px;
                  color: var(--text-secondary);
                  margin-top: 0;
                  line-height: 1.4;
                "
              >
                高级参数（如 uuid, tls, network 等），将作为 JSON
                对象合并到该节点配置中。<br />
                解析链接后，这里会预先填充。如需手动输入格式请使用合法的 JSON。
              </p>
              <codemirror
                v-model="configString"
                :extensions="cmExtensions"
                :style="{
                  width: '100%',
                  maxHeight: '200px',
                  fontSize: '13px',
                  borderRadius: '8px',
                }"
                :indent-with-tab="true"
                :tab-size="2"
              />
            </el-form-item>
          </el-form>
          <div style="margin-top: 20px; text-align: right">
            <el-button @click="nodeDialogVisible = false" plain>取消</el-button>
            <el-button
              type="success"
              @click="saveCustomNode"
              :loading="isSubmittingNode"
            >
              {{ editingNodeId ? "确认并更新云端" : "确认并存入云端" }}
            </el-button>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <!-- 自定义规则弹窗 -->
    <el-dialog
      v-model="ruleDialogVisible"
      :title="editingRuleId ? '✏️ 编辑云端自定义规则' : '✏️ 新增 / 接管分流规则'"
      width="550px"
      class="glass-dialog"
    >
      <el-form label-position="top">
        <el-form-item label="规则类型 (Type)">
          <el-select v-model="newRuleForm.type" filterable allow-create default-first-option style="width: 100%" popper-class="glass-dropdown">
            <el-option v-for="t in ruleTypes" :key="t" :label="t" :value="t" />
          </el-select>
        </el-form-item>
        <el-form-item label="匹配内容 (Payload) - MATCH 可留空">
          <el-input
            v-model="newRuleForm.payload"
            placeholder="例如：google.com"
          ></el-input>
        </el-form-item>
        <el-form-item label="目标策略 (Target)">
          <el-select
            v-model="newRuleForm.target"
            filterable
            allow-create
            default-first-option
            placeholder="请选择或输入目标策略"
            style="width: 100%"
            popper-class="glass-dropdown"
          >
            <el-option
              v-for="option in builtInRuleTargetOptions"
              :key="option.value"
              :label="option.label"
              :value="option.value"
            />
            <el-option-group label="现有策略组">
              <el-option
                v-for="g in proxyGroups"
                :key="g.name"
                :label="g.name"
                :value="g.name"
              />
            </el-option-group>
            <el-option-group label="现有独立节点">
              <el-option
                v-for="n in parsedNodes"
                :key="n.name"
                :label="n.name"
                :value="n.name"
              />
            </el-option-group>
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleDialogVisible = false" plain>取消</el-button>
        <el-button
          type="primary"
          @click="saveCustomRule"
          :loading="isSubmittingRule"
        >
          {{ editingRuleId ? "更新并立即云端同步" : "保存并立即覆盖同步" }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 最终订阅链接弹窗 -->
    <el-dialog
      v-model="subLinkDialogVisible"
      :title="subLinkDialogTitle"
      width="720px"
      class="glass-dialog sub-link-dialog-shell"
    >
      <div class="sub-link-dialog">
        <div class="sub-link-dialog__intro">
          <div class="sub-link-dialog__profile">
            <span>当前配置</span>
            <strong>{{ currentProfileName }}</strong>
          </div>
          <p>
            按客户端选择对应地址。Clash / Mihomo 使用默认 YAML 订阅，Surge 使用专用 .conf 配置，
            Shadowrocket 优先使用安装入口创建新配置。
          </p>
        </div>

        <div v-if="showRegeneratedWarning" class="sub-link-warning" role="alert">
          <strong>旧 token 已失效</strong>
          <span>重新生成订阅会覆盖旧 token，请及时替换客户端中的旧订阅地址。</span>
        </div>

        <div class="sub-link-grid">
          <section class="sub-link-card sub-link-card--primary">
            <div class="sub-link-card__header">
              <div>
                <h3>Clash / Mihomo</h3>
                <p>默认 YAML 订阅地址，适合 Clash Verge、Mihomo Party 等客户端。</p>
              </div>
              <span class="sub-link-card__tag">YAML</span>
            </div>
            <div class="sub-link-row">
              <el-input v-model="finalSubLink" readonly class="copy-input" />
              <IconTooltipButton
                label="复制 Clash / Mihomo 订阅地址"
                type="primary"
                :circle="false"
                :icon="CopyDocument"
                @click="copySubLink"
              >
                复制
              </IconTooltipButton>
            </div>
          </section>

          <section class="sub-link-card">
            <div class="sub-link-card__header">
              <div>
                <h3>Surge 最新版</h3>
                <p>新版 Surge 专用配置地址，输出最新兼容格式。</p>
              </div>
              <span class="sub-link-card__tag sub-link-card__tag--warning">CONF</span>
            </div>
            <div class="sub-link-row">
              <el-input v-model="surgeLatestSubLink" readonly class="copy-input" />
              <IconTooltipButton
                label="复制 Surge 最新版配置地址"
                type="warning"
                :circle="false"
                :icon="CopyDocument"
                @click="copySurgeLatestSubLink"
              >
                复制
              </IconTooltipButton>
            </div>
          </section>

          <section class="sub-link-card">
            <div class="sub-link-card__header">
              <div>
                <h3>Surge 5.7.6</h3>
                <p>面向 Surge 5.7.6 的兼容配置地址。</p>
              </div>
              <span class="sub-link-card__tag sub-link-card__tag--warning">兼容</span>
            </div>
            <div class="sub-link-row">
              <el-input v-model="surge576SubLink" readonly class="copy-input" />
              <IconTooltipButton
                label="复制 Surge 5.7.6 兼容配置地址"
                type="warning"
                :circle="false"
                :icon="CopyDocument"
                @click="copySurge576SubLink"
              >
                复制
              </IconTooltipButton>
            </div>
          </section>

          <section class="sub-link-card sub-link-card--shadowrocket">
            <div class="sub-link-card__header">
              <div>
                <h3>Shadowrocket</h3>
                <p>优先安装到 Shadowrocket；安装失败时再复制配置地址到 iOS 配置里下载。</p>
              </div>
              <IconTooltipButton
                label="安装到 Shadowrocket"
                type="success"
                :circle="false"
                :icon="Link"
                @click="installShadowrocketConfig"
              >
                安装
              </IconTooltipButton>
            </div>
            <div class="sub-link-stack">
              <div class="sub-link-field">
                <span>安装链接</span>
                <div class="sub-link-row">
                  <el-input v-model="shadowrocketInstallLink" readonly class="copy-input" />
                  <IconTooltipButton
                    label="复制 Shadowrocket 安装链接"
                    type="success"
                    :circle="false"
                    :icon="CopyDocument"
                    @click="copyShadowrocketInstallLink"
                  >
                    复制
                  </IconTooltipButton>
                </div>
              </div>
              <div class="sub-link-field">
                <span>配置地址</span>
                <div class="sub-link-row">
                  <el-input v-model="shadowrocketSubLink" readonly class="copy-input" />
                  <IconTooltipButton
                    label="复制 Shadowrocket 配置地址"
                    type="success"
                    :circle="false"
                    :icon="CopyDocument"
                    @click="copyShadowrocketSubLink"
                  >
                    复制
                  </IconTooltipButton>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
      <template #footer>
        <el-button
          class="sub-link-close-button"
          @click="subLinkDialogVisible = false"
          type="primary"
        >
          关闭
        </el-button>
      </template>
    </el-dialog>

    <!-- 页脚版权说明 -->
    <footer class="main-footer">
      <p>
        Clash Subscription Decoder © 2026. Built with Gin & Vue 3 +
        Element Plus.
      </p>
    </footer>

    <!-- 订阅抓取代理池设置 -->
    <el-dialog
      v-model="proxySettingsVisible"
      title="订阅抓取代理池"
      width="680px"
      align-center
      class="glass-dialog"
      :close-on-click-modal="!isLoadingProxySettings && !isSavingProxySettings"
      :close-on-press-escape="!isLoadingProxySettings && !isSavingProxySettings"
      :show-close="!isLoadingProxySettings && !isSavingProxySettings"
      @closed="clearProxySettingsState"
    >
      <div v-loading="isLoadingProxySettings" class="proxy-settings-content">
        <el-form label-position="top">
          <el-form-item label="是否使用代理抓取订阅">
            <div class="proxy-switch-row">
              <el-switch
                v-model="proxySettingsForm.proxy_enabled"
                active-text="已开启"
                inactive-text="未开启"
                :disabled="isLoadingProxySettings || isSavingProxySettings"
              />
              <span>开启后，系统会从代理池中选择出口拉取远程订阅。</span>
            </div>
          </el-form-item>

          <el-form-item label="代理池（一行一个）">
            <el-input
              v-model="proxySettingsForm.proxy_pool"
              type="textarea"
              :rows="8"
              resize="vertical"
              autocomplete="off"
              autocapitalize="off"
              autocorrect="off"
              :spellcheck="false"
              :disabled="!proxySettingsForm.proxy_enabled || isLoadingProxySettings || isSavingProxySettings"
              placeholder="hostname:port:username:password&#10;socks5://username:password@host:port"
            />
            <div class="proxy-format-help">
              <p>支持以下四种格式；未填写 <code>socks5://</code> 协议前缀时按 HTTP 代理处理。</p>
              <code>hostname:port:username:password</code>
              <code>socks5://username:password@host:port</code>
              <code>username:password@hostname:port</code>
              <code>hostname:port@username:password</code>
            </div>
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button
          plain
          :disabled="isLoadingProxySettings || isSavingProxySettings"
          @click="proxySettingsVisible = false"
        >
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="isSavingProxySettings"
          :disabled="isLoadingProxySettings"
          @click="saveProxySettings"
        >
          保存设置
        </el-button>
      </template>
    </el-dialog>

    <!-- 修改密码对话框 -->
    <el-dialog
      v-model="changePasswordVisible"
      title="🔑 修改管理员密码"
      width="400px"
      align-center
      class="glass-dialog"
    >
      <el-form 
        :model="passwordForm" 
        :rules="passwordRules" 
        ref="passwordFormRef" 
        label-position="top"
        style="padding: 10px 0"
      >
        <el-form-item label="当前密码" prop="oldPassword">
          <el-input 
            v-model="passwordForm.oldPassword" 
            type="password" 
            show-password 
            placeholder="请输入当前密码" 
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input 
            v-model="passwordForm.newPassword" 
            type="password" 
            show-password 
            placeholder="请输入新密码 (最小5位)" 
          />
        </el-form-item>
        <el-form-item label="确认新密码" prop="confirmPassword">
          <el-input 
            v-model="passwordForm.confirmPassword" 
            type="password" 
            show-password 
            placeholder="请再次输入新密码" 
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="changePasswordVisible = false" plain>取消</el-button>
        <el-button 
          type="primary" 
          :loading="isChangingPassword" 
          @click="submitChangePassword"
        >
          确认修改
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.iconfont-symbol-sprite {
  position: absolute;
  width: 0;
  height: 0;
  overflow: hidden;
}

/* 精美的版面自适应样式 */
.app-container {
  max-width: 1200px;
  width: 100%;
  margin: 0 auto;
  padding: 30px 20px;
  display: flex;
  flex-direction: column;
  gap: 30px;
  min-height: 100vh;
  box-sizing: border-box;
}

/* 导航栏样式 */
.main-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 20px 30px;
  border-radius: 20px;
}

.logo-wrapper {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-icon {
  font-size: 28px;
  animation: float-icon 3s ease-in-out infinite alternate;
}

@keyframes float-icon {
  0% {
    transform: translateY(0) scale(1);
  }
  100% {
    transform: translateY(-4px) scale(1.1);
  }
}

.logo-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 1px;
}

.status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}

.pulse-dot {
  width: 8px;
  height: 8px;
  background-color: #10b981;
  border-radius: 50%;
  box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
  }
  70% {
    transform: scale(1);
    box-shadow: 0 0 0 8px rgba(16, 185, 129, 0);
  }
  100% {
    transform: scale(0.95);
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
  }
}

/* 主体内容 */
.main-content {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.profiles-panel,
.control-panel {
  padding: 30px;
  text-align: left;
}

.profiles-header {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: flex-start;
}

.profiles-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.profiles-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 14px;
}

.profile-card {
  width: 100%;
  border: 1px solid var(--glass-border);
  border-radius: 8px;
  padding: 14px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  transition: border-color 0.2s ease, transform 0.2s ease, background 0.2s ease;
}

.profile-card:hover,
.profile-card.is-active {
  border-color: rgba(99, 102, 241, 0.7);
  background: rgba(99, 102, 241, 0.12);
  transform: translateY(-1px);
}

.profile-card-main {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
}

.profile-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 700;
}

.profile-meta {
  min-height: 20px;
  margin-top: 8px;
  color: var(--text-secondary);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-source-summary {
  display: grid;
  gap: 6px;
  margin-top: 10px;
}

.profile-source-chip {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 8px;
  min-height: 26px;
  padding: 5px 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.36);
}

.profile-source-chip.is-primary {
  border-color: rgba(56, 189, 248, 0.38);
  background: rgba(56, 189, 248, 0.1);
}

.profile-source-role {
  color: #e0f2fe;
  font-size: 11px;
  font-weight: 700;
  white-space: nowrap;
}

.profile-source-url {
  min-width: 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.profile-actions {
  display: flex;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 8px;
}

.profile-source-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
}

.profile-source-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 10px;
  align-items: center;
}

.source-primary-btn {
  min-width: 104px;
}

.profile-source-row :deep(.el-button--primary.is-plain),
.profile-source-footer :deep(.el-button--primary.is-plain) {
  --el-button-text-color: #e0f2fe;
  --el-button-bg-color: rgba(var(--color-primary-rgb), 0.18);
  --el-button-border-color: rgba(var(--color-primary-rgb), 0.45);
  --el-button-hover-text-color: #ffffff;
  --el-button-hover-bg-color: var(--color-primary);
  --el-button-hover-border-color: var(--color-primary);
  --el-button-active-text-color: #ffffff;
  --el-button-active-bg-color: #0284c7;
  --el-button-active-border-color: #0284c7;
}

.profile-source-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.profile-source-tip {
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.5;
}

.section-title {
  margin: 0 0 10px 0;
  font-size: 22px;
  font-weight: 600;
  color: var(--text-primary);
}

.section-desc {
  margin: 0 0 24px 0;
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.6;
}

.input-area {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.decode-input {
  font-size: 16px;
}

.active-source-strip {
  display: grid;
  gap: 8px;
}

.active-source-item {
  display: grid;
  grid-template-columns: 86px minmax(0, 1fr);
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.42);
}

.active-source-item.is-primary {
  border-color: rgba(34, 197, 94, 0.36);
  background: rgba(34, 197, 94, 0.08);
}

.active-source-item span {
  color: #bbf7d0;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.active-source-item strong {
  min-width: 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.input-prefix-icon {
  color: var(--color-primary);
  font-size: 18px;
}

.button-group {
  display: flex;
  gap: 14px;
}

.manual-config-actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.action-btn {
  padding: 20px 28px !important;
  font-size: 15px !important;
}

.mock-btn {
  border-radius: 12px !important;
  padding: 20px 24px !important;
  font-size: 14px !important;
  background-color: rgba(255, 255, 255, 0.02) !important;
  border-color: rgba(255, 255, 255, 0.08) !important;
  color: var(--text-secondary) !important;
  transition: all 0.3s !important;
}

.mock-btn:hover {
  background-color: rgba(255, 255, 255, 0.06) !important;
  color: var(--text-primary) !important;
  border-color: rgba(255, 255, 255, 0.18) !important;
}

.error-alert {
  margin-top: 16px;
}

/* 结果看板 */
.result-panel {
  padding: 30px;
}

.result-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px;
  margin-bottom: 24px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  padding-bottom: 20px;
}

.meta-info {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
}

.meta-item {
  display: flex;
  align-items: center;
  font-size: 14px;
}

.meta-label {
  color: var(--text-secondary);
}

.result-actions {
  display: flex;
  justify-content: flex-end;
  max-width: 100%;
}

.result-action-group {
  display: inline-flex;
  max-width: 100%;
  overflow: hidden;
  border: 1px solid rgba(148, 163, 184, 0.18);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.42);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.05),
    0 10px 24px rgba(0, 0, 0, 0.18);
}

.result-action-group :deep(.el-button) {
  height: 38px;
  margin-left: 0 !important;
  padding: 0 14px !important;
  border: 0 !important;
  border-radius: 0 !important;
  background: transparent !important;
  color: var(--text-secondary) !important;
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0;
  transition:
    background-color 0.2s ease,
    color 0.2s ease;
}

.result-action-group :deep(.el-button + .el-button) {
  border-left: 1px solid rgba(148, 163, 184, 0.14) !important;
}

.result-action-group :deep(.el-button:hover),
.result-action-group :deep(.el-button:focus) {
  background: rgba(56, 189, 248, 0.12) !important;
  color: #e0f2fe !important;
}

.result-action-group :deep(.result-action-button--link:hover),
.result-action-group :deep(.result-action-button--link:focus) {
  background: rgba(250, 204, 21, 0.12) !important;
  color: #fef3c7 !important;
}

.result-action-group :deep(.result-action-button--danger) {
  color: #fca5a5 !important;
}

.result-action-group :deep(.result-action-button--danger:hover),
.result-action-group :deep(.result-action-button--danger:focus) {
  background: rgba(248, 113, 113, 0.12) !important;
  color: #fecaca !important;
}

@media (max-width: 768px) {
  .profiles-header {
    flex-direction: column;
  }

  .profiles-actions,
  .profiles-actions :deep(.el-button),
  .manual-config-actions,
  .manual-config-actions :deep(.el-button) {
    width: 100%;
  }

  .result-actions {
    width: 100%;
  }

  .result-action-group {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    width: 100%;
  }

  .result-action-group :deep(.el-button) {
    width: 100%;
    padding: 0 10px !important;
    justify-content: center;
  }

  .result-action-group :deep(.el-button + .el-button) {
    border-left: 0 !important;
  }

  .result-action-group :deep(.el-button:nth-child(2n)) {
    border-left: 1px solid rgba(148, 163, 184, 0.14) !important;
  }

  .result-action-group :deep(.el-button:nth-child(n + 3)) {
    border-top: 1px solid rgba(148, 163, 184, 0.14) !important;
  }

  .profile-source-row {
    grid-template-columns: 1fr;
  }

  .profile-source-row :deep(.el-button),
  .profile-source-footer :deep(.el-button) {
    width: 100%;
  }

  .profile-source-footer {
    align-items: stretch;
  }

  .active-source-item {
    grid-template-columns: 1fr;
    gap: 4px;
  }

  .provider-grid {
    grid-template-columns: 1fr;
  }
}

.custom-tabs :deep(.el-tabs__item) {
  color: var(--text-secondary) !important;
  font-size: 15px !important;
  font-weight: 500 !important;
  padding: 0 20px !important;
  height: 50px !important;
  line-height: 50px !important;
  transition: color 0.3s !important;
}

.custom-tabs :deep(.el-tabs__item.is-active) {
  color: var(--color-primary) !important;
}

.custom-tabs :deep(.el-tabs__active-bar) {
  background-color: var(--color-primary) !important;
  height: 3px !important;
  border-radius: 2px !important;
}

.custom-tabs :deep(.el-tabs__nav-wrap::after) {
  background-color: rgba(255, 255, 255, 0.05) !important;
}

.code-wrapper {
  margin-top: 20px;
}

.raw-base64-text {
  color: #a78bfa;
}

/* 节点网格卡片系统 */
.nodes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 16px;
  margin-top: 20px;
}

.node-card {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid rgba(255, 255, 255, 0.04);
  border-radius: 12px;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  text-align: left;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.node-card:hover {
  transform: translateY(-4px);
  background: rgba(255, 255, 255, 0.04);
  border-color: rgba(99, 102, 241, 0.25);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
}

.node-card.is-drag-over,
.group-card.is-drag-over {
  border-color: rgba(56, 189, 248, 0.65);
  box-shadow: 0 0 0 1px rgba(56, 189, 248, 0.25), 0 10px 26px rgba(0, 0, 0, 0.28);
}

.node-card.is-order-saving,
.group-card.is-order-saving {
  opacity: 0.78;
}

.node-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.03);
  padding-bottom: 8px;
}

.drag-handle {
  width: 24px;
  height: 24px;
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 6px;
  background: rgba(255, 255, 255, 0.04);
  color: var(--text-muted);
  cursor: grab;
  user-select: none;
  line-height: 1;
}

.drag-handle:hover {
  color: #38bdf8;
  border-color: rgba(56, 189, 248, 0.45);
  background: rgba(56, 189, 248, 0.1);
}

.drag-handle:active {
  cursor: grabbing;
}

.drag-handle.disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.node-flag {
  font-size: 20px;
}

.node-name {
  font-weight: 600;
  font-size: 14px;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-grow: 1;
}

.node-card-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 13px;
}

.node-info-row {
  display: flex;
  justify-content: space-between;
}

.info-label {
  color: var(--text-muted);
}

.info-val {
  color: var(--text-secondary);
  max-width: 160px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.highlight-port {
  color: #38bdf8;
  font-weight: 500;
}

.node-card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 4px;
}

.cipher-label {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--font-mono);
  background: rgba(255, 255, 255, 0.03);
  padding: 2px 6px;
  border-radius: 4px;
}

/* 骨架屏容器 */
.skeleton-wrapper {
  padding: 30px;
}

/* 页脚 */
.main-footer {
  margin-top: auto;
  color: var(--text-muted);
  font-size: 13px;
  padding: 20px 0;
  border-top: 1px solid rgba(255, 255, 255, 0.03);
}

/* 动画定义 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

.slide-up-enter-active {
  transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);
}
.slide-up-enter-from {
  opacity: 0;
  transform: translateY(20px);
}

/* CodeMirror 高颜值卡片包裹 */
.editor-glass-wrapper {
  margin-top: 20px;
  border-radius: 12px;
  overflow: hidden;
  box-shadow:
    inset 0 2px 10px rgba(0, 0, 0, 0.5),
    0 4px 15px rgba(0, 0, 0, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.editor-glass-wrapper :deep(.cm-editor) {
  background-color: rgba(0, 0, 0, 0.45) !important;
  font-family: var(--font-mono) !important;
  height: 100%;
}

.editor-glass-wrapper :deep(.cm-scroller) {
  font-family: var(--font-mono) !important;
  padding: 10px 0;
}

.editor-glass-wrapper :deep(.cm-gutters) {
  background-color: rgba(255, 255, 255, 0.02) !important;
  border-right: 1px solid rgba(255, 255, 255, 0.05) !important;
  color: var(--text-muted) !important;
}

.editor-glass-wrapper :deep(.cm-activeLine),
.editor-glass-wrapper :deep(.cm-activeLineGutter) {
  background-color: rgba(255, 255, 255, 0.04) !important;
}

.provider-panel,
.diagnostics-panel {
  display: grid;
  gap: 16px;
  margin-top: 20px;
}

.provider-summary,
.diagnostics-metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}

.provider-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 14px;
}

.provider-card {
  display: grid;
  gap: 14px;
  padding: 16px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.46);
}

.provider-card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.provider-title {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.provider-title span {
  overflow: hidden;
  color: var(--text-primary);
  font-size: 15px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.provider-title small {
  color: var(--text-secondary);
  font-size: 12px;
}

.provider-detail-list {
  display: grid;
  gap: 8px;
}

.provider-detail-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
  color: var(--text-secondary);
  font-size: 12px;
}

.provider-detail-row span {
  color: rgba(226, 232, 240, 0.58);
}

.provider-detail-row strong {
  min-width: 0;
  overflow: hidden;
  color: #dbeafe;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.diagnostics-panel :deep(.el-alert) {
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.42);
}

.diagnostics-panel :deep(.el-alert__title) {
  color: var(--text-primary);
  font-weight: 700;
}

.diagnostics-panel :deep(.el-alert__description) {
  color: var(--text-secondary);
  line-height: 1.6;
}

/* 代理组卡片系统 */
.groups-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 20px;
  margin-top: 20px;
}

.group-card {
  background: rgba(13, 18, 30, 0.5);
  border: 1px solid rgba(255, 255, 255, 0.05);
  border-radius: 14px;
  padding: 18px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  transition: all 0.3s ease;
}

.group-card:hover {
  border-color: rgba(99, 102, 241, 0.3);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
  transform: translateY(-2px);
}

.group-card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
  padding-bottom: 12px;
}

.group-card-main {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  gap: 8px;
  overflow: hidden;
}

.group-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--color-primary);
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.group-type-tag {
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.shadowrocket-badge {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.96);
  border: 1px solid rgba(124, 114, 234, 0.36);
  box-shadow:
    0 8px 18px rgba(79, 195, 247, 0.14),
    inset 0 0 0 1px rgba(255, 255, 255, 0.68);
}

.iconfont-shadowrocket {
  display: block;
  width: 20px;
  height: 20px;
}

.group-card:hover .shadowrocket-badge {
  border-color: rgba(79, 195, 247, 0.54);
  box-shadow:
    0 10px 24px rgba(124, 114, 234, 0.2),
    inset 0 0 0 1px rgba(255, 255, 255, 0.78);
}

.group-card-body {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  max-height: 180px;
  overflow-y: auto;
  padding-right: 4px;
}

.proxy-tag {
  background: rgba(255, 255, 255, 0.03) !important;
  border-color: rgba(255, 255, 255, 0.08) !important;
  color: var(--text-secondary) !important;
}

/* 规则表格系统 */
.rules-container {
  margin-top: 20px;
  padding: 20px;
  background: rgba(0, 0, 0, 0.2) !important;
}

.rules-toolbar {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 20px;
}

.rules-toolbar-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}

.rules-filter-group {
  display: flex;
  align-items: center;
  flex: 1 1 560px;
  min-width: 0;
  gap: 10px;
}

.rule-target-filter {
  flex: 0 0 220px;
}

.rule-search-input {
  flex: 1 1 320px;
  min-width: 260px;
  --el-input-bg-color: rgba(255, 255, 255, 0.05) !important;
}

.rules-primary-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}

.rules-primary-actions :deep(.el-button + .el-button),
.rules-batch-controls :deep(.el-button + .el-button) {
  margin-left: 0 !important;
}

.rules-more-button {
  border-color: rgba(148, 163, 184, 0.2) !important;
  background: rgba(15, 23, 42, 0.36) !important;
  color: var(--text-secondary) !important;
}

.rules-more-button:hover,
.rules-more-button:focus {
  border-color: rgba(56, 189, 248, 0.36) !important;
  color: #e0f2fe !important;
  background: rgba(56, 189, 248, 0.1) !important;
}

.rules-batch-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 12px;
  border: 1px solid rgba(148, 163, 184, 0.12);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.26);
}

.rules-batch-copy {
  display: flex;
  flex-direction: column;
  min-width: 180px;
  gap: 2px;
}

.rules-batch-title {
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 700;
}

.rules-batch-desc {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.rules-batch-controls {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.rule-bulk-target-select {
  width: 280px;
  max-width: 100%;
}

:deep(.rules-danger-dropdown-item:not(.is-disabled)) {
  color: #fca5a5 !important;
}

:deep(.rules-danger-dropdown-item:not(.is-disabled):hover) {
  background-color: rgba(248, 113, 113, 0.12) !important;
  color: #fecaca !important;
}

@media (max-width: 900px) {
  .rules-toolbar-main,
  .rules-batch-panel {
    align-items: stretch;
    flex-direction: column;
  }

  .rules-filter-group,
  .rules-primary-actions,
  .rules-batch-controls {
    width: 100%;
  }

  .rules-primary-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 640px) {
  .rules-filter-group,
  .rules-batch-controls {
    flex-direction: column;
    align-items: stretch;
  }

  .rule-target-filter,
  .rule-search-input,
  .rule-bulk-target-select,
  .rules-primary-actions :deep(.el-button),
  .rules-primary-actions :deep(.el-dropdown),
  .rules-primary-actions :deep(.el-dropdown .el-button),
  .rules-batch-controls :deep(.el-button) {
    width: 100%;
  }

  .rule-target-filter,
  .rule-search-input {
    flex: 1 1 auto;
    min-width: 0;
  }

  .rules-primary-actions {
    display: grid;
    grid-template-columns: 1fr;
  }

  .provider-detail-row {
    grid-template-columns: 1fr;
    gap: 3px;
  }
}

.custom-table {
  --el-table-bg-color: transparent !important;
  --el-table-tr-bg-color: transparent !important;
  --el-table-header-bg-color: rgba(255, 255, 255, 0.03) !important;
  --el-table-border-color: rgba(255, 255, 255, 0.05) !important;
  --el-table-row-hover-bg-color: rgba(255, 255, 255, 0.04) !important;
  --el-table-text-color: var(--text-secondary) !important;
  --el-table-header-text-color: var(--text-primary) !important;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.05);
}

.rule-type-tag {
  font-family: var(--font-mono);
  letter-spacing: 0.5px;
}

.rule-target-tag {
  font-weight: 600;
}

.rule-payload {
  font-family: var(--font-mono);
  color: #a78bfa;
}

.pagination-wrapper {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
  padding: 10px 0 0 0;
}

.pagination-wrapper :deep(.el-pagination) {
  --el-pagination-bg-color: rgba(255, 255, 255, 0.05);
  --el-pagination-text-color: var(--text-secondary);
  --el-pagination-button-color: var(--text-secondary);
  --el-pagination-button-disabled-bg-color: transparent;
  --el-pagination-hover-color: var(--color-primary);
}

.pagination-wrapper :deep(.el-pager li) {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.05);
  border-radius: 6px;
}

.pagination-wrapper :deep(.el-pager li.is-active) {
  background-color: var(--color-primary) !important;
  color: #fff !important;
  border-color: var(--color-primary) !important;
}

.pagination-wrapper :deep(.el-input__wrapper) {
  background-color: rgba(255, 255, 255, 0.05) !important;
  box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.1) inset !important;
}

/* 玻璃拟物弹窗定制 */
:deep(.glass-dialog) {
  background: rgba(13, 18, 30, 0.85) !important;
  backdrop-filter: blur(20px) !important;
  border: 1px solid rgba(255, 255, 255, 0.1) !important;
  border-radius: 16px !important;
  box-shadow: 0 24px 50px rgba(0, 0, 0, 0.5) !important;
}

:deep(.glass-dialog .el-dialog__title) {
  color: var(--text-primary) !important;
  font-weight: 600;
}

:deep(.glass-dialog .el-form-item__label) {
  color: var(--text-secondary) !important;
}

:deep(.glass-dropdown) {
  background: rgba(13, 18, 30, 0.95) !important;
  backdrop-filter: blur(10px) !important;
  border: 1px solid rgba(255, 255, 255, 0.1) !important;
}

:deep(.glass-dropdown .el-select-group__title) {
  color: var(--color-primary) !important;
  font-weight: bold;
}

.app-bootstrap {
  width: 100%;
  min-height: 100vh;
  padding: 30px 20px;
  box-sizing: border-box;
  background-color: var(--bg-primary);
}

.bootstrap-shell {
  width: 100%;
  max-width: 1200px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.bootstrap-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  padding: 20px 30px;
  border-radius: 20px;
}

.bootstrap-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.bootstrap-logo {
  flex: 0 0 auto;
  font-size: 28px;
  animation: float-icon 3s ease-in-out infinite alternate;
}

.bootstrap-brand-title {
  color: var(--text-primary);
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 1px;
}

.bootstrap-brand-subtitle {
  margin-top: 4px;
  color: var(--text-secondary);
  font-size: 13px;
}

.bootstrap-status {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  color: #bfdbfe;
  font-size: 14px;
  font-weight: 600;
}

.bootstrap-panel {
  padding: 30px;
}

.bootstrap-copy {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  margin-bottom: 24px;
}

.bootstrap-icon {
  flex: 0 0 auto;
  margin-top: 4px;
  color: #38bdf8;
  font-size: 26px;
}

.bootstrap-copy h2 {
  margin: 0 0 8px 0;
  color: var(--text-primary);
  font-size: 22px;
  font-weight: 600;
}

.bootstrap-copy p {
  margin: 0;
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.6;
}

.bootstrap-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 20px;
}

.bootstrap-card {
  padding: 24px;
}

@media (max-width: 768px) {
  .app-bootstrap {
    padding: 20px 14px;
  }

  .bootstrap-header {
    align-items: flex-start;
    flex-direction: column;
    padding: 20px;
  }

  .bootstrap-brand-title {
    font-size: 17px;
  }

  .bootstrap-panel,
  .bootstrap-card {
    padding: 20px;
  }

  .bootstrap-grid {
    grid-template-columns: 1fr;
  }
}

/* 用户下拉菜单精致样式 */
.user-dropdown {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: var(--text-primary);
  padding: 4px 10px;
  border-radius: 12px;
  transition: all 0.3s ease;
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid rgba(255, 255, 255, 0.05);
}

.user-dropdown:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: rgba(56, 189, 248, 0.2);
}

.user-avatar {
  background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%) !important;
  color: #ffffff !important;
  font-weight: 600;
  font-size: 13px;
}

.username-text {
  font-size: 14px;
  font-weight: 500;
  color: var(--text-secondary);
}

/* 弹出层全局样式统一定制 */
:deep(.el-dropdown__popper) {
  --el-dropdown-menu-box-shadow: 0 10px 30px -5px rgba(0, 0, 0, 0.6) !important;
}

:deep(.el-dropdown-menu) {
  background-color: rgba(13, 18, 30, 0.95) !important;
  border: 1px solid rgba(255, 255, 255, 0.08) !important;
  backdrop-filter: blur(15px) !important;
  border-radius: 14px !important;
  padding: 6px !important;
}

:deep(.el-dropdown-menu__item) {
  color: var(--text-secondary) !important;
  border-radius: 8px !important;
  padding: 8px 16px !important;
  font-size: 13px !important;
  transition: all 0.2s ease !important;
}

:deep(.el-dropdown-menu__item:hover) {
  background-color: rgba(56, 189, 248, 0.1) !important;
  color: #38bdf8 !important;
}

:deep(.el-dropdown-menu__item.el-dropdown-menu__item--divided) {
  border-top-color: rgba(255, 255, 255, 0.05) !important;
}

.proxy-settings-content {
  min-height: 330px;
}

.proxy-switch-row {
  display: flex;
  align-items: center;
  gap: 14px;
  width: 100%;
}

.proxy-switch-row span {
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.proxy-format-help {
  display: grid;
  gap: 6px;
  width: 100%;
  margin-top: 10px;
  padding-left: 12px;
  border-left: 3px solid rgba(56, 189, 248, 0.45);
  color: var(--text-secondary);
}

.proxy-format-help p {
  margin: 0 0 2px;
  font-size: 12px;
  line-height: 1.6;
}

.proxy-format-help code {
  max-width: 100%;
  overflow-wrap: anywhere;
  color: #bae6fd;
  font-family: "Cascadia Code", Consolas, monospace;
  font-size: 12px;
  line-height: 1.5;
}

/* 专业控制台响应式改版覆盖层 */
.icon-action-row,
.dialog-icon-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.dialog-icon-actions {
  margin-bottom: 12px;
}

.dialog-icon-actions :deep(.el-button.is-plain) {
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.08);
}

.dialog-icon-actions :deep(.el-button--primary.is-plain) {
  --el-button-text-color: #bfdbfe !important;
  --el-button-bg-color: rgba(37, 99, 235, 0.16) !important;
  --el-button-border-color: rgba(96, 165, 250, 0.46) !important;
  --el-button-hover-text-color: #ffffff !important;
  --el-button-hover-bg-color: #2563eb !important;
  --el-button-hover-border-color: #3b82f6 !important;
  --el-button-active-text-color: #ffffff !important;
  --el-button-active-bg-color: #1d4ed8 !important;
  --el-button-active-border-color: #2563eb !important;
  background: var(--el-button-bg-color) !important;
  border-color: var(--el-button-border-color) !important;
  color: var(--el-button-text-color) !important;
}

.dialog-icon-actions :deep(.el-button.is-disabled) {
  --el-button-disabled-text-color: rgba(148, 163, 184, 0.5) !important;
  --el-button-disabled-bg-color: rgba(15, 23, 42, 0.28) !important;
  --el-button-disabled-border-color: rgba(148, 163, 184, 0.14) !important;
  box-shadow: none;
}

.dialog-icon-actions--end {
  justify-content: flex-end;
  margin-top: 15px;
}

.app-container {
  max-width: 1280px;
  padding: 24px;
  gap: 22px;
}

.main-header {
  position: sticky;
  top: 16px;
  z-index: 10;
  padding: 16px 20px;
}

.header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  min-width: 0;
}

.profiles-panel,
.control-panel,
.result-panel {
  padding: 22px;
}

.profiles-header {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
}

.profiles-actions,
.manual-config-actions,
.button-group,
.rules-primary-actions,
.rules-batch-controls {
  gap: 8px;
}

.profile-card {
  border-color: rgba(148, 163, 184, 0.14);
  background: rgba(15, 23, 42, 0.52);
}

.profile-actions,
.custom-actions {
  align-items: center;
}

.result-action-group {
  padding: 4px;
  border-radius: 12px;
}

.result-action-group :deep(.el-button) {
  width: 40px;
  height: 40px;
  padding: 0 !important;
  border-radius: 10px !important;
}

.nodes-header-actions,
.groups-header-actions {
  display: flex !important;
  align-items: center !important;
  justify-content: flex-end !important;
  gap: 8px !important;
  flex-wrap: wrap;
}

.rules-more-button,
.mobile-more-button {
  width: 40px;
  height: 40px;
  border-color: rgba(148, 163, 184, 0.2) !important;
  background: rgba(15, 23, 42, 0.36) !important;
  color: var(--text-secondary) !important;
}

.rules-more-button:hover,
.rules-more-button:focus,
.mobile-more-button:hover,
.mobile-more-button:focus {
  border-color: rgba(56, 189, 248, 0.38) !important;
  color: #e0f2fe !important;
  background: rgba(56, 189, 248, 0.1) !important;
}

.sub-link-dialog-shell {
  --dialog-width: 720px;
}

.sub-link-dialog {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 2px 0;
}

.sub-link-dialog__intro {
  display: grid;
  gap: 10px;
  padding: 14px;
  border: 1px solid rgba(56, 189, 248, 0.16);
  border-radius: 14px;
  background: rgba(14, 165, 233, 0.06);
}

.sub-link-dialog__intro p {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.65;
}

.sub-link-dialog__profile {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--text-secondary);
  font-size: 13px;
}

.sub-link-dialog__profile strong {
  min-width: 0;
  color: var(--text-primary);
  font-size: 15px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub-link-warning {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 14px;
  border: 1px solid rgba(239, 68, 68, 0.26);
  border-radius: 12px;
  background: rgba(239, 68, 68, 0.08);
  color: #fecaca;
  font-size: 13px;
  line-height: 1.5;
}

.sub-link-warning strong {
  flex: 0 0 auto;
  color: #fca5a5;
}

.sub-link-grid {
  display: grid;
  gap: 12px;
}

.sub-link-card {
  display: grid;
  gap: 12px;
  padding: 14px;
  border: 1px solid rgba(148, 163, 184, 0.14);
  border-radius: 14px;
  background: rgba(15, 23, 42, 0.46);
}

.sub-link-card--primary {
  border-color: rgba(56, 189, 248, 0.22);
  background: rgba(8, 47, 73, 0.22);
}

.sub-link-card--shadowrocket {
  border-color: rgba(16, 185, 129, 0.2);
}

.sub-link-card__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.sub-link-card__header h3 {
  margin: 0;
  color: var(--text-primary);
  font-size: 15px;
  line-height: 1.35;
}

.sub-link-card__header p {
  margin: 4px 0 0;
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.55;
}

.sub-link-card__tag {
  flex: 0 0 auto;
  padding: 4px 8px;
  border-radius: 999px;
  background: rgba(14, 165, 233, 0.12);
  color: #7dd3fc;
  font-size: 11px;
  font-weight: 700;
  line-height: 1;
}

.sub-link-card__tag--warning {
  background: rgba(245, 158, 11, 0.14);
  color: #fcd34d;
}

.sub-link-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: stretch;
  gap: 10px;
}

.sub-link-row :deep(.el-button) {
  min-width: 88px;
  min-height: 40px;
  padding: 0 14px !important;
  border-radius: 10px !important;
}

.copy-input {
  min-width: 0;
}

.copy-input :deep(.el-input__wrapper) {
  min-height: 40px;
  padding: 4px 12px !important;
}

.copy-input :deep(.el-input__inner) {
  font-family: var(--font-mono);
  font-size: 12px;
}

.sub-link-stack {
  display: grid;
  gap: 10px;
}

.sub-link-field {
  display: grid;
  gap: 7px;
}

.sub-link-field > span {
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.mobile-action-bar {
  display: none;
}

:deep(.app-tooltip) {
  max-width: min(260px, calc(100vw - 32px));
}

.form-label-with-tooltip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.form-label-help {
  color: var(--text-secondary);
  cursor: help;
}

:deep(.glass-dialog) {
  width: min(var(--dialog-width, 720px), calc(100vw - 32px)) !important;
  max-height: calc(100vh - 32px);
  margin: 16px auto !important;
  display: flex;
  flex-direction: column;
}

:deep(.glass-dialog .el-dialog__body) {
  min-height: 0;
  overflow: auto;
  padding: 18px 22px;
}

:deep(.glass-dialog .el-dialog__footer) {
  padding: 14px 22px 18px;
  border-top: 1px solid rgba(148, 163, 184, 0.12);
}

.sub-link-dialog-shell :deep(.sub-link-close-button) {
  --el-button-text-color: #ffffff;
  --el-button-bg-color: #0ea5e9;
  --el-button-border-color: #0ea5e9;
  --el-button-hover-text-color: #ffffff;
  --el-button-hover-bg-color: #0284c7;
  --el-button-hover-border-color: #0284c7;
  --el-button-active-text-color: #ffffff;
  --el-button-active-bg-color: #0369a1;
  --el-button-active-border-color: #0369a1;
  border-color: #0ea5e9 !important;
  background: #0ea5e9 !important;
  color: #ffffff !important;
}

.sub-link-dialog-shell :deep(.sub-link-close-button:hover),
.sub-link-dialog-shell :deep(.sub-link-close-button:focus) {
  border-color: #0284c7 !important;
  background: #0284c7 !important;
  color: #ffffff !important;
}

.sub-link-dialog-shell :deep(.sub-link-close-button:active) {
  border-color: #0369a1 !important;
  background: #0369a1 !important;
  color: #ffffff !important;
}

@media (max-width: 900px) {
  .app-container {
    padding: 18px 14px 92px;
  }

  .main-header {
    position: static;
    align-items: flex-start;
    gap: 14px;
  }

  .logo-title {
    font-size: 16px;
  }

  .status-indicator {
    margin-right: 0 !important;
  }

  .profiles-header {
    grid-template-columns: 1fr;
  }

  .profiles-actions,
  .manual-config-actions {
    justify-content: flex-start;
  }

  .rules-container {
    padding: 14px;
  }

  .custom-table {
    overflow-x: auto;
  }
}

@media (max-width: 640px) {
  .app-container {
    padding: 12px 10px 92px;
    gap: 14px;
  }

  .main-header,
  .profiles-panel,
  .control-panel,
  .result-panel,
  .skeleton-wrapper {
    padding: 14px;
    border-radius: 12px;
  }

  .main-header {
    flex-direction: column;
  }

  .header-actions {
    width: 100%;
    justify-content: space-between;
  }

  .username-text,
  .status-indicator {
    display: none !important;
  }

  .section-title {
    font-size: 18px;
  }

  .section-desc {
    margin-bottom: 16px;
    font-size: 13px;
  }

  .profiles-grid,
  .nodes-grid,
  .groups-grid {
    grid-template-columns: 1fr;
  }

  .result-header,
  .rules-toolbar-main,
  .rules-batch-panel {
    align-items: stretch;
  }

  .meta-info {
    gap: 10px;
  }

  .result-actions,
  .result-action-group,
  .rules-filter-group,
  .rules-primary-actions,
  .rules-batch-controls {
    width: 100%;
  }

  .profiles-actions :deep(.el-button),
  .manual-config-actions :deep(.el-button),
  .result-action-group :deep(.el-button),
  .rules-primary-actions :deep(.el-button),
  .rules-batch-controls :deep(.el-button),
  .dialog-icon-actions :deep(.el-button) {
    width: 42px;
    min-width: 42px;
  }

  .result-action-group,
  .rules-primary-actions,
  .rules-batch-controls {
    display: flex;
    justify-content: flex-start;
  }

  .custom-tabs :deep(.el-tabs__nav-wrap) {
    overflow-x: auto;
  }

  .custom-tabs :deep(.el-tabs__item) {
    padding: 0 12px !important;
    font-size: 13px !important;
    white-space: nowrap;
  }

  .custom-table :deep(.el-table__body-wrapper),
  .custom-table :deep(.el-table__header-wrapper) {
    overflow-x: auto;
  }

  .pagination-wrapper {
    justify-content: flex-start;
    overflow-x: auto;
  }

  .sub-link-dialog {
    gap: 12px;
  }

  .sub-link-dialog__intro,
  .sub-link-card {
    padding: 12px;
    border-radius: 12px;
  }

  .sub-link-dialog__profile {
    align-items: flex-start;
    flex-direction: column;
    gap: 4px;
  }

  .sub-link-dialog__profile strong {
    max-width: 100%;
    white-space: normal;
    word-break: break-word;
  }

  .sub-link-warning {
    flex-direction: column;
    gap: 4px;
    padding: 10px 12px;
  }

  .sub-link-card__header {
    flex-direction: column;
    gap: 10px;
  }

  .sub-link-card__header :deep(.el-button) {
    width: 100%;
    min-height: 42px;
  }

  .sub-link-row {
    grid-template-columns: 1fr;
    gap: 8px;
  }

  .sub-link-row :deep(.el-button) {
    width: 100%;
    min-height: 42px;
  }

  .copy-input :deep(.el-input__wrapper) {
    min-height: 42px;
  }

  .mobile-action-bar {
    position: fixed;
    left: 10px;
    right: 10px;
    bottom: max(10px, env(safe-area-inset-bottom));
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: space-around;
    gap: 8px;
    padding: 8px;
    border-radius: 14px;
  }

  :deep(.glass-dialog) {
    width: calc(100vw - 20px) !important;
    max-height: calc(100vh - 20px);
    margin: 10px auto !important;
    border-radius: 12px !important;
  }

  :deep(.glass-dialog .el-dialog__header),
  :deep(.glass-dialog .el-dialog__body),
  :deep(.glass-dialog .el-dialog__footer) {
    padding-left: 14px;
    padding-right: 14px;
  }

  :deep(.glass-dialog .el-dialog__footer .el-button) {
    min-height: 40px;
  }

  .proxy-settings-content {
    min-height: 390px;
  }

  .proxy-switch-row {
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
  }
}
</style>
