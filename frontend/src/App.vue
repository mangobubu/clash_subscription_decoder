<script setup lang="ts">
import { ref, computed, onMounted, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Link, Edit, Delete, Loading, ArrowDown } from "@element-plus/icons-vue";
import axios from "axios";
import { Codemirror } from "vue-codemirror";
import { yaml as codemirrorYaml } from "@codemirror/lang-yaml";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import jsYaml from "js-yaml";
import Login from "./Login.vue";
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
  has_token: boolean;
  created_at: number;
  updated_at: number;
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
});

const currentProfile = computed(() =>
  profiles.value.find((profile) => profile.id === activeProfileId.value) || null,
);

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
        inputUrl.value = nextProfile?.url || "";
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
  if (tab === "rules") return (parsedConfig.value?.rules?.length || 0) > 0;
  return tab === "text" || tab === "raw";
}

function applyActiveTabAfterSubscriptionLoad(options: LoadSubscriptionOptions, previousTab: string) {
  const preferredTab = options.preferredTab || (options.preserveActiveTab ? previousTab : "");
  if (preferredTab && isResultTabAvailable(preferredTab)) {
    activeTab.value = preferredTab;
    return;
  }
  activeTab.value = parsedNodes.value.length > 0 ? "nodes" : "text";
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
    inputUrl.value = currentProfile.value?.url || "";
  }
};

const selectProfile = async (profile: SubscriptionProfile) => {
  if (activeProfileId.value === profile.id) return;
  activeProfileId.value = profile.id;
  localStorage.setItem("active_profile_id", String(profile.id));
  inputUrl.value = profile.url || "";
  errorMsg.value = "";
  dirtyRulesMap.value = {};
  await fetchCustomData();
  await loadSubscription();
};

const openCreateProfileDialog = () => {
  editingProfileId.value = null;
  profileForm.value = {
    name: "",
    source_type: "remote",
    url: "",
  };
  profileDialogVisible.value = true;
};

const openEditProfileDialog = (profile: SubscriptionProfile) => {
  editingProfileId.value = profile.id;
  profileForm.value = {
    name: profile.name,
    source_type: profile.source_type,
    url: profile.url || "",
  };
  profileDialogVisible.value = true;
};

const saveProfile = async () => {
  if (!profileForm.value.name.trim()) {
    ElMessage.warning("请输入配置名称");
    return;
  }
  if (profileForm.value.source_type === "remote" && !profileForm.value.url.trim()) {
    ElMessage.warning("请输入远程订阅地址");
    return;
  }
  isSubmittingProfile.value = true;
  try {
    const payload = { ...profileForm.value, local_content: "" };
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
  try {
    const res = await axios.post(buildBackendUrl(`/api/profiles/${activeProfileId.value}/refresh`));
    if (res.data.code === 200) {
      result.value = res.data.data;
      hasSubscription.value = true;
      inputUrl.value = res.data.data.url || "";
      await loadProfiles(activeProfileId.value);
      await fetchCustomData();
      ElMessage.success("当前配置已刷新");
      activeTab.value = parsedNodes.value.length > 0 ? "nodes" : "text";
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
      // 如果解析出来的节点数大于0，默认跳到 nodes 页签，否则跳到 text
      if (parsedNodes.value.length > 0) {
        activeTab.value = "nodes";
      } else {
        activeTab.value = "text";
      }
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

// 解析代理节点
const parsedNodes = computed<ProxyNode[]>(() => {
  if (!parsedConfig.value || !parsedConfig.value.proxies) return [];
  return parsedConfig.value.proxies.map((p: any) => ({
    name: p.name,
    type: p.type,
    server: p.server,
    port: p.port,
    details: p,
  }));
});

// 代理组解析
const proxyGroups = computed<any[]>(() => {
  if (!parsedConfig.value || !parsedConfig.value["proxy-groups"]) return [];
  return parsedConfig.value["proxy-groups"];
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

const getRuleIdentityKey = (row: Pick<RuleDisplayRow, "type" | "payload">) =>
  row.payload === "-" ? row.type : `${row.type},${row.payload}`;

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
  if (!parsedConfig.value || !parsedConfig.value.rules) return [];
  const targets = new Set<string>();
  parsedConfig.value.rules.forEach((ruleStr: string) => {
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
  if (!parsedConfig.value || !parsedConfig.value.rules) return [];
  let rules = parsedConfig.value.rules.map((ruleStr: string) => parseRuleForDisplay(ruleStr));

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

const newGroupForm = ref({
  name: "",
  type: "select",
  proxies: [] as string[],
  exclude: "",
});

const groupTypes = ["select", "url-test", "fallback", "load-balance"];
const builtInGroupProxies = [{ label: "DIRECT (直连)", value: "DIRECT" }];

const openGroupDialog = () => {
  editingGroupId.value = null;
  newGroupForm.value = { name: "", type: "select", proxies: [], exclude: "" };
  groupDialogVisible.value = true;
};

const editCustomGroup = (groupName: string) => {
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
  };
  groupDialogVisible.value = true;
};

const deleteCustomGroup = (groupName: string) => {
  const customInfo = customGroupsDict.value[groupName];
  if (!customInfo) return;
  ElMessageBox.confirm(
    `确定要删除自定义策略组 [${groupName}] 吗？`,
    "安全提示",
    {
      confirmButtonText: "确定删除",
      cancelButtonText: "取消",
      type: "warning",
    },
  )
    .then(async () => {
      try {
        const res = await axios.delete(
          buildBackendUrl(`/api/custom-groups/${customInfo.ID}`),
        );
        if (res.data.code === 200) {
          ElMessage.success("自定义策略组已成功删除！");
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

const editCustomNode = (nodeName: string) => {
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

const deleteCustomNode = (nodeName: string) => {
  const customInfo = customNodesDict.value[nodeName];
  if (!customInfo) return;
  ElMessageBox.confirm(
    `确定要彻底删除自定义节点 [${nodeName}] 吗？`,
    "安全提示",
    {
      confirmButtonText: "立即销毁",
      cancelButtonText: "取消保留",
      type: "warning",
    },
  )
    .then(async () => {
      try {
        const res = await axios.delete(
          buildBackendUrl(`/api/custom-nodes/${customInfo.ID}`),
        );
        if (res.data.code === 200) {
          ElMessage.success("自定义节点已被彻底删除！");
          await fetchCustomData();
          if (activeProfileId.value) {
            await loadSubscription();
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
        await loadSubscription();
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
const hasDirtyRules = computed(() => Object.keys(dirtyRulesMap.value).length > 0);

const markRuleDirty = (row: any) => {
  dirtyRulesMap.value[getRuleIdentityKey(row)] = row;
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
    const promises = Object.values(dirtyRulesMap.value).map((row: any) => {
      let key = getRuleIdentityKey(row);
      let customInfo = customRulesDict.value[key];
      let submitData = {
        profile_id: activeProfileId.value,
        type: row.type,
        payload: row.payload === "-" ? "-" : row.payload,
        target: row.target,
      };
      if (customInfo) {
        return axios.put(buildBackendUrl(`/api/custom-rules/${customInfo.ID}`), submitData);
      } else {
        return axios.post(buildBackendUrl("/api/custom-rules"), submitData);
      }
    });

    await Promise.all(promises);
    ElMessage.success(`成功批量接管 ${promises.length} 条策略！正在重新拉取订阅...`);
    dirtyRulesMap.value = {};
    await fetchCustomData();
    if (activeProfileId.value) {
      await loadSubscription({ preferredTab: "rules" });
    }
  } catch (error: any) {
    ElMessage.error("部分策略保存失败: " + (error.response?.data?.message || error.message));
  } finally {
    isSubmittingRule.value = false;
  }
};

// ---------------------- 自定义规则管理逻辑 ----------------------
const ruleDialogVisible = ref(false);
const isSubmittingRule = ref(false);
const editingRuleId = ref<number | null>(null);
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

const openRuleDialog = () => {
  editingRuleId.value = null;
  newRuleForm.value = { type: "DOMAIN-SUFFIX", payload: "", target: "PROXY" };
  ruleDialogVisible.value = true;
};

const editRule = (row: any) => {
  let key = getRuleIdentityKey(row);
  let customInfo = customRulesDict.value[key];

  if (customInfo) {
    editingRuleId.value = customInfo.ID;
    newRuleForm.value = {
      type: customInfo.Type,
      payload: customInfo.Payload === "-" ? "" : customInfo.Payload,
      target: customInfo.Target,
    };
  } else {
    editingRuleId.value = null; // 接管原生规则
    newRuleForm.value = {
      type: row.type,
      payload: row.payload === "-" ? "" : row.payload,
      target: row.target,
    };
  }
  ruleDialogVisible.value = true;
};

const deleteCustomRule = (row: any) => {
  let key = getRuleIdentityKey(row);
  let customInfo = customRulesDict.value[key];
  if (!customInfo) return;

  ElMessageBox.confirm(`确定要移除对该规则的接管并恢复原生状态吗？`, "安全提示", {
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
          ElMessage.success("自定义分流规则已成功移除！");
          await fetchCustomData();
          if (activeProfileId.value) {
            await loadSubscription();
          }
        }
      } catch (err: any) {
        ElMessage.error("移除失败: " + err.message);
      }
    })
    .catch(() => {});
};

const saveCustomRule = async () => {
  if (!newRuleForm.value.type || !newRuleForm.value.target) {
    ElMessage.warning("请补全规则类型和目标策略");
    return;
  }
  if (newRuleForm.value.type !== "MATCH" && !newRuleForm.value.payload) {
    ElMessage.warning("请输入匹配内容 (Payload)");
    return;
  }
  if (!activeProfileId.value) {
    ElMessage.warning("请先选择一个配置");
    return;
  }

  let submitData = { ...newRuleForm.value, profile_id: activeProfileId.value };
  if (!submitData.payload) {
    submitData.payload = "-";
  }

  isSubmittingRule.value = true;
  try {
    let res;
    if (editingRuleId.value) {
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
      ElMessage.success(
        editingRuleId.value ? "规则更新成功！" : "规则已云端接管生效！"
      );
      ruleDialogVisible.value = false;
      await fetchCustomData();
      if (activeProfileId.value) {
        await loadSubscription();
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
  return !!customRulesDict.value[getRuleIdentityKey(row)];
};

// ---------------------- 个人中心 (修改密码与退出登录) ----------------------
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
  } else if (command === "backupData") {
    try {
      const res = await axios.get(buildBackendUrl("/api/backup"), {
        responseType: "blob",
      });
      const url = window.URL.createObjectURL(new Blob([res.data]));
      const link = document.createElement("a");
      link.href = url;
      link.setAttribute("download", `clash_proxy_backup_${new Date().getTime()}.json`);
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
              <el-dropdown-item command="backupData">⬇️ 备份数据</el-dropdown-item>
              <el-dropdown-item command="importData">⬆️ 导入备份</el-dropdown-item>
              <el-dropdown-item divided command="changePassword">🔑 修改密码</el-dropdown-item>
              <el-dropdown-item divided command="logout">🚪 退出登录</el-dropdown-item>
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
	          <div class="profiles-actions">
	            <el-button type="primary" icon="Plus" @click="openCreateProfileDialog">
	              新增配置
	            </el-button>
	            <el-button
	              :disabled="!currentProfile"
	              icon="Refresh"
	              :loading="isLoading"
	              @click="refreshCurrentProfile"
	            >
	              刷新当前
	            </el-button>
	          </div>
	          <div v-if="currentProfile?.source_type === 'local'" class="manual-config-actions">
	            <el-button type="primary" plain icon="Plus" @click="openNodeDialog">
	              添加节点
	            </el-button>
	            <el-button type="success" plain icon="Plus" @click="openGroupDialog">
	              添加代理组
	            </el-button>
	            <el-button type="warning" plain icon="Plus" @click="openRuleDialog">
	              添加规则
	            </el-button>
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
	              <el-tag size="small" :type="profile.source_type === 'local' ? 'success' : 'primary'">
	                {{ profile.source_type === 'local' ? '本地手动' : '远程订阅' }}
	              </el-tag>
	            </div>
	            <div class="profile-meta">
	              {{ profile.source_type === 'local' ? '不依赖订阅地址' : profile.url }}
	            </div>
	            <div class="profile-actions" @click.stop>
	              <el-button size="small" icon="Edit" text @click="openEditProfileDialog(profile)">
	                编辑
	              </el-button>
	              <el-button size="small" icon="Delete" text type="danger" @click="deleteProfile(profile)">
	                删除
	              </el-button>
	            </div>
	          </button>
	        </div>
	        <el-empty v-else description="暂无配置，请先新增一个远程订阅或本地手动配置" />
	      </section>

	      <!-- 控制卡片面板 -->
	      <section class="control-panel glass-card">
	        <h2 class="section-title">
	          {{ currentProfile?.source_type === 'local' ? '本地手动配置预览' : '自适应 Base64 地址获取器' }}
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
	            :disabled="isLoading || currentProfile?.source_type === 'local'"
	            class="decode-input"
	          >
            <template #prefix>
              <el-icon class="input-prefix-icon"><Link /></el-icon>
            </template>
          </el-input>

          <div class="button-group">
	            <el-button
	              v-if="hasSubscription"
	              type="success"
	              @click="currentProfile?.source_type === 'local' ? refreshCurrentProfile() : handleDecode()"
	              :loading="isLoading"
	              class="action-btn"
	            >
	              {{ currentProfile?.source_type === 'local' ? '生成本地订阅' : '刷新订阅' }}
	            </el-button>
	            <el-button
	              v-else
	              type="primary"
	              @click="currentProfile?.source_type === 'local' ? refreshCurrentProfile() : handleDecode()"
	              :loading="isLoading"
	              class="action-btn"
	            >
	              {{ currentProfile?.source_type === 'local' ? '生成本地预览' : '一键抓取并解码' }}
	            </el-button>
	            <el-button
	              type="info"
	              plain
	              @click="handleQuickMock"
	              :disabled="isLoading || currentProfile?.source_type === 'local'"
	              class="mock-btn"
	            >
              Mock 快速测试
            </el-button>
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
            </div>

            <div class="result-actions">
              <el-button-group class="result-action-group">
                <el-button
                  size="default"
                  icon="CopyDocument"
                  class="result-action-button result-action-button--copy"
                  @click="handleCopy"
                >
                  复制明文
                </el-button>
                <el-button
                  size="default"
                  icon="Download"
                  class="result-action-button result-action-button--export"
                  @click="handleDownload"
                >
                  导出 YAML
                </el-button>
                <el-button
                  size="default"
                  icon="Link"
                  class="result-action-button result-action-button--link"
                  @click="copyCurrentSubLink"
                  :loading="isCopyingSubLink"
                >
                  复制订阅地址
                </el-button>
                <el-button
                  size="default"
                  icon="Refresh"
                  class="result-action-button result-action-button--danger"
                  @click="regenerateSubLink"
                  :loading="isGeneratingSubLink"
                >
                  重新生成订阅
                </el-button>
              </el-button-group>
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
                <el-button
                  type="success"
                  effect="dark"
                  round
                  @click="openNodeDialog"
                >
                  <span style="margin-right: 4px; font-weight: bold">+</span> ✨
                  新增自定义节点
                </el-button>
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
                    <div
                      v-if="customNodesDict[node.name]"
                      class="custom-actions"
                      style="display: flex; gap: 4px"
                    >
                      <el-button
                        type="primary"
                        link
                        :icon="Edit"
                        @click="editCustomNode(node.name)"
                        title="编辑云端节点"
                      ></el-button>
                      <el-button
                        type="danger"
                        link
                        :icon="Delete"
                        @click="deleteCustomNode(node.name)"
                        title="删除云端节点"
                      ></el-button>
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
                <el-button
                  type="primary"
                  effect="dark"
                  round
                  @click="openGroupDialog"
                >
                  <span style="margin-right: 4px; font-weight: bold">+</span>
                  新增自定义策略组
                </el-button>
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
                    <div
                      style="
                        display: flex;
                        align-items: center;
                        overflow: hidden;
                        flex: 1;
                        gap: 8px;
                      "
                    >
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
                    </div>
                    <div
                      v-if="customGroupsDict[group.name]"
                      class="custom-actions"
                      style="display: flex; gap: 4px"
                    >
                      <el-button
                        type="primary"
                        link
                        :icon="Edit"
                        @click="editCustomGroup(group.name)"
                        title="编辑策略组"
                      ></el-button>
                      <el-button
                        type="danger"
                        link
                        :icon="Delete"
                        @click="deleteCustomGroup(group.name)"
                        title="删除策略组"
                      ></el-button>
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
            <el-tab-pane name="rules" v-if="parsedConfig?.rules?.length > 0">
              <template #label>
                <span class="tab-label"
                  >📋 分流规则 ({{ parsedConfig.rules.length }})</span
                >
              </template>

              <div class="rules-container glass-card">
                <div class="rules-toolbar" style="display: flex; flex-wrap: wrap; align-items: center; gap: 15px;">
                  <el-select
                    v-model="ruleTargetFilter"
                    placeholder="目标策略过滤"
                    clearable
                    filterable
                    style="width: 220px;"
                    popper-class="glass-dropdown"
                  >
                    <el-option label="[全部策略]" value="" />
                    <el-option v-for="t in ruleTargets" :key="t" :label="t" :value="t" />
                  </el-select>
                  <el-select
                    v-model="bulkRuleTarget"
                    placeholder="批量设置为目标策略"
                    clearable
                    filterable
                    allow-create
                    default-first-option
                    style="width: 240px;"
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
                  <el-button
                    type="primary"
                    plain
                    round
                    :disabled="!bulkRuleTarget || parsedRules.length === 0"
                    @click="applyBulkRuleTargetToFilteredRules"
                  >
                    一键设置筛选结果
                  </el-button>
                  <el-input
                    v-model="ruleSearchQuery"
                    placeholder="输入关键字进一步检索规则类型或内容..."
                    clearable
                    class="rule-search-input"
                    style="flex: 1;"
                  >
                    <template #prefix>
                      <span style="font-size: 16px">🔍</span>
                    </template>
                  </el-input>
                  <el-button
                    v-if="hasDirtyRules"
                    type="success"
                    effect="dark"
                    round
                    :loading="isSubmittingRule"
                    @click="batchSaveRules"
                  >
                    💾 批量应用修改 ({{ Object.keys(dirtyRulesMap).length }})
                  </el-button>
                  <el-button
                    type="primary"
                    effect="dark"
                    round
                    @click="openRuleDialog"
                  >
                    <span style="margin-right: 4px; font-weight: bold">+</span>
                    新增自定义规则
                  </el-button>
                  <el-button
                    v-if="currentProfile?.source_type === 'remote'"
                    type="success"
                    plain
                    round
                    :loading="isLocalizingRules"
                    @click="localizeRemoteRules"
                  >
                    本地化远程规则
                  </el-button>
                  <el-button
                    type="warning"
                    plain
                    round
                    :disabled="copyRuleSourceOptions.length === 0"
                    @click="openCopyRulesDialog"
                  >
                    从其他配置复制规则
                  </el-button>
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
                  <el-table-column label="操作" width="120" fixed="right">
                    <template #default="scope">
                      <div style="display: flex; gap: 4px">
                        <el-button
                          type="primary"
                          link
                          :icon="Edit"
                          @click="editRule(scope.row)"
                          :title="isCustomRule(scope.row) ? '高级编辑' : '高级编辑并接管'"
                        ></el-button>
                        <el-button
                          v-if="isCustomRule(scope.row)"
                          type="danger"
                          link
                          :icon="Delete"
                          @click="deleteCustomRule(scope.row)"
                          title="移除接管 (恢复原生)"
                        ></el-button>
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
	          <el-input
	            v-model="profileForm.url"
	            placeholder="https://example.com/sub"
	            clearable
	          />
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
        <el-form-item label="配置包含的目标代理">
          <div style="margin-bottom: 12px; display: flex; gap: 10px">
            <el-button
              size="small"
              type="primary"
              plain
              @click="selectAllNodes"
            >
              注入全部最新节点 [ALL_NODES]
            </el-button>
            <el-button
              size="small"
              type="info"
              plain
              @click="selectAllExistingGroups"
            >
              引入所有现有策略组
            </el-button>
            <el-button
              size="small"
              type="success"
              plain
              @click="selectDirectPolicy"
            >
              加入 DIRECT 直连
            </el-button>
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
            <div style="margin-top: 15px; text-align: right">
              <el-button
                type="primary"
                @click="parseNodeLink"
                :loading="isParsingLink"
                >一键解析链接</el-button
              >
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
      width="640px"
      class="glass-dialog"
    >
	      <div style="padding: 10px 0;">
	        <p style="color: var(--text-secondary); margin-bottom: 15px; font-size: 14px; line-height: 1.5;">
	          当前配置：<strong style="color: var(--text-primary);">{{ currentProfileName }}</strong><br/>
	          请选择要使用的客户端配置地址。Clash/Mihomo 使用默认 YAML 订阅；Surge 最新版与 Surge 5.7.6 使用各自专用 .conf 配置地址；Shadowrocket 请优先点击“安装到 Shadowrocket”创建新配置，失败时再复制配置地址到 iOS 的“配置”里通过 URL 下载，不要作为首页普通服务器订阅导入。
	          <template v-if="showRegeneratedWarning">
            <br/><br/>
            <strong style="color: var(--el-color-danger);">重新生成订阅会覆盖旧 token，旧订阅地址将立即失效。</strong>
          </template>
        </p>
        <div style="display: grid; gap: 14px;">
          <div>
            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary); margin-bottom: 8px;">
              Clash / Mihomo 订阅地址
            </div>
            <el-input
              v-model="finalSubLink"
              readonly
              class="copy-input"
            >
              <template #append>
                <el-button icon="CopyDocument" @click="copySubLink" type="primary">复制</el-button>
              </template>
            </el-input>
          </div>
          <div>
            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary); margin-bottom: 8px;">
              Surge 最新版配置地址
            </div>
            <el-input
              v-model="surgeLatestSubLink"
              readonly
              class="copy-input"
            >
              <template #append>
                <el-button icon="CopyDocument" @click="copySurgeLatestSubLink" type="warning">复制</el-button>
              </template>
            </el-input>
          </div>
          <div>
            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary); margin-bottom: 8px;">
              Surge 5.7.6 兼容配置地址
            </div>
            <el-input
              v-model="surge576SubLink"
              readonly
              class="copy-input"
            >
              <template #append>
                <el-button icon="CopyDocument" @click="copySurge576SubLink" type="warning">复制</el-button>
              </template>
            </el-input>
          </div>
          <div>
            <div style="font-size: 13px; font-weight: 700; color: var(--text-primary); margin-bottom: 8px;">
              Shadowrocket 配置地址
            </div>
            <el-button
              type="success"
              icon="Link"
              style="width: 100%; margin-bottom: 10px;"
              @click="installShadowrocketConfig"
            >
              安装到 Shadowrocket
            </el-button>
            <el-input
              v-model="shadowrocketInstallLink"
              readonly
              class="copy-input"
              style="margin-bottom: 10px;"
            >
              <template #append>
                <el-button icon="CopyDocument" @click="copyShadowrocketInstallLink" type="success">复制安装链接</el-button>
              </template>
            </el-input>
            <el-input
              v-model="shadowrocketSubLink"
              readonly
              class="copy-input"
            >
              <template #append>
                <el-button icon="CopyDocument" @click="copyShadowrocketSubLink" type="success">复制</el-button>
              </template>
            </el-input>
          </div>
        </div>
      </div>
      <template #footer>
        <el-button @click="subLinkDialogVisible = false" type="primary" plain>我知道了</el-button>
      </template>
    </el-dialog>

    <!-- 页脚版权说明 -->
    <footer class="main-footer">
      <p>
        Base64 Subscription Analyzer & Decoder © 2026. Built with Gin & Vue 3 +
        Element Plus.
      </p>
    </footer>

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

.profile-actions {
  display: flex;
  justify-content: flex-end;
  gap: 4px;
  margin-top: 8px;
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

.group-name {
  font-size: 16px;
  font-weight: 600;
  color: var(--color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.group-type-tag {
  text-transform: uppercase;
  letter-spacing: 0.5px;
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
  margin-bottom: 20px;
}

.rule-search-input {
  max-width: 400px;
  --el-input-bg-color: rgba(255, 255, 255, 0.05) !important;
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
</style>
