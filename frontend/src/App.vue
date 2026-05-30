<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link, Edit, Delete } from '@element-plus/icons-vue'
import axios from 'axios'
import { Codemirror } from 'vue-codemirror'
import { yaml as codemirrorYaml } from '@codemirror/lang-yaml'
import { oneDark } from '@codemirror/theme-one-dark'
import { EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import jsYaml from 'js-yaml'

// CodeMirror 配置
const cmExtensions = [
  codemirrorYaml(),
  oneDark,
  EditorState.readOnly.of(true),
  EditorView.lineWrapping,
]

// 状态定义
const inputUrl = ref('')
const isLoading = ref(false)
const activeTab = ref('nodes')
const errorMsg = ref('')
const result = ref<{ url: string; raw_base64: string; decoded: string } | null>(null)

// ---------------------- 自定义资源字典状态 ----------------------
const customNodesDict = ref<Record<string, any>>({})
const customGroupsDict = ref<Record<string, any>>({})

const fetchCustomData = async () => {
  try {
    const [nodesRes, groupsRes] = await Promise.all([
      axios.get('http://localhost:8080/api/custom-nodes'),
      axios.get('http://localhost:8080/api/custom-groups')
    ])
    const nDict: Record<string, any> = {}
    if (nodesRes.data.code === 200) {
      nodesRes.data.data.forEach((n: any) => nDict[n.Name || n.name] = n)
    }
    customNodesDict.value = nDict

    const gDict: Record<string, any> = {}
    if (groupsRes.data.code === 200) {
      groupsRes.data.data.forEach((g: any) => gDict[g.Name || g.name] = g)
    }
    customGroupsDict.value = gDict
  } catch(e) {
    console.error('获取自定义数据失败', e)
  }
}

onMounted(() => {
  fetchCustomData()
})

// 节点接口定义
interface ProxyNode {
  name: string;
  type: string;
  server: string;
  port: string | number;
  details: Record<string, any>;
}

// 快速填入 Mock 地址
const handleQuickMock = () => {
  inputUrl.value = 'mock.clash.local/sub'
  handleDecode()
}

// 清除输入
const handleClear = () => {
  inputUrl.value = ''
  result.value = null
  errorMsg.value = ''
}

// 解析并获取 Base64 内容
const handleDecode = async () => {
  const url = inputUrl.value.trim()
  if (!url) {
    ElMessage.warning('请输入订阅或配置地址')
    return
  }

  isLoading.value = true
  errorMsg.value = ''
  result.value = null

  try {
    const response = await axios.post('http://localhost:8080/api/decode', { url })
    if (response.data && response.data.code === 200) {
      result.value = response.data.data
      await fetchCustomData()
      ElMessage.success('成功拉取并完成 Base64 解码！')
      // 如果解析出来的节点数大于0，默认跳到 nodes 页签，否则跳到 text
      if (parsedNodes.value.length > 0) {
        activeTab.value = 'nodes'
      } else {
        activeTab.value = 'text'
      }
    } else {
      throw new Error(response.data.message || '未知错误')
    }
  } catch (error: any) {
    console.error(error)
    let msg = '网络连接失败，请检查后端服务是否正常启动 (http://localhost:8080)'
    if (error.response && error.response.data) {
      msg = error.response.data.message || msg
      if (error.response.data.error) {
        msg += ` (${error.response.data.error})`
      }
    } else if (error.message) {
      msg = error.message
    }
    errorMsg.value = msg
    ElMessage.error('获取或解码失败')
  } finally {
    isLoading.value = false
  }
}

// 核心响应式配置对象
const parsedConfig = computed<any>(() => {
  if (!result.value || !result.value.decoded) return null
  try {
    return jsYaml.load(result.value.decoded)
  } catch (err) {
    console.error('YAML 解析失败:', err)
    return null
  }
})

// 解析代理节点
const parsedNodes = computed<ProxyNode[]>(() => {
  if (!parsedConfig.value || !parsedConfig.value.proxies) return []
  return parsedConfig.value.proxies.map((p: any) => ({
    name: p.name,
    type: p.type,
    server: p.server,
    port: p.port,
    details: p
  }))
})

// 代理组解析
const proxyGroups = computed<any[]>(() => {
  if (!parsedConfig.value || !parsedConfig.value['proxy-groups']) return []
  return parsedConfig.value['proxy-groups']
})

// 规则搜索关键字
const ruleSearchQuery = ref('')

// 规则分页状态
const currentRulePage = ref(1)
const rulePageSize = ref(100)

// 监听搜索词变化，重置页码
import { watch } from 'vue'
watch(ruleSearchQuery, () => {
  currentRulePage.value = 1
})

// 分流规则解析 (拆解 DOMAIN-SUFFIX,google.com,PROXY)
const parsedRules = computed<any[]>(() => {
  if (!parsedConfig.value || !parsedConfig.value.rules) return []
  let rules = parsedConfig.value.rules.map((ruleStr: string) => {
    const parts = ruleStr.split(',')
    return {
      raw: ruleStr,
      type: parts[0] || 'UNKNOWN',
      payload: parts.length > 2 ? parts[1] : '-',
      target: parts.length > 2 ? parts[2] : (parts[1] || '-')
    }
  })
  
  if (ruleSearchQuery.value) {
    const q = ruleSearchQuery.value.toLowerCase()
    rules = rules.filter((r: any) => 
      r.type.toLowerCase().includes(q) || 
      r.payload.toLowerCase().includes(q) || 
      r.target.toLowerCase().includes(q)
    )
  }
  
  return rules
})

// 当前页显示的规则
const paginatedRules = computed(() => {
  const start = (currentRulePage.value - 1) * rulePageSize.value
  const end = start + rulePageSize.value
  return parsedRules.value.slice(start, end)
})

// 根据节点名称自适应匹配国旗表情包
const getFlagEmoji = (name: string): string => {
  const n = name.toUpperCase()
  if (n.includes('香港') || n.includes('HK') || n.includes('HONGKONG')) return '🇭🇰'
  if (n.includes('新加坡') || n.includes('SG') || n.includes('SINGAPORE')) return '🇸🇬'
  if (n.includes('日本') || n.includes('东京') || n.includes('JP') || n.includes('JAPAN') || n.includes('TOKYO')) return '🇯🇵'
  if (n.includes('美国') || n.includes('US') || n.includes('UNITED STATES') || n.includes('美')) return '🇺🇸'
  if (n.includes('台湾') || n.includes('TW') || n.includes('TAIWAN')) return '🇹🇼'
  if (n.includes('韩国') || n.includes('首尔') || n.includes('KR') || n.includes('KOREA')) return '🇰🇷'
  if (n.includes('英国') || n.includes('UK') || n.includes('GB') || n.includes('ENGLAND')) return '🇬🇧'
  if (n.includes('德国') || n.includes('DE') || n.includes('GERMANY')) return '🇩🇪'
  if (n.includes('俄罗斯') || n.includes('RU') || n.includes('RUSSIA')) return '🇷🇺'
  return '🌐'
}

// 节点协议色彩主题
const getNodeTypeTag = (type: string) => {
  const t = type.toLowerCase()
  if (t === 'ss' || t === 'shadowsocks') return { type: 'warning', label: 'SS' }
  if (t === 'vmess') return { type: 'danger', label: 'VMESS' }
  if (t === 'vless') return { type: 'success', label: 'VLESS' }
  if (t === 'trojan') return { type: 'primary', label: 'TROJAN' }
  if (t === 'ssr' || t === 'shadowsocksr') return { type: 'info', label: 'SSR' }
  return { type: 'info', label: type.toUpperCase() }
}

// 统计信息
const stats = computed(() => {
  if (!result.value) return { size: 0, lines: 0 }
  const size = new Blob([result.value.decoded]).size
  const lines = result.value.decoded.split('\n').length
  return { size, lines }
})

// 一键复制
const handleCopy = async () => {
  if (!result.value) return
  try {
    await navigator.clipboard.writeText(result.value.decoded)
    ElMessage.success('配置内容已成功复制到剪贴板！')
  } catch (err) {
    ElMessage.error('复制失败，请手动选择复制')
  }
}

// 导出下载文件
const handleDownload = () => {
  if (!result.value) return
  const blob = new Blob([result.value.decoded], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `clash_decoded_${Date.now()}.yaml`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  ElMessage.success('成功导出本地配置文件！')
}

// ---------------------- 自定义组管理逻辑 ----------------------
const groupDialogVisible = ref(false)
const isSubmittingGroup = ref(false)
const editingGroupId = ref<number | null>(null)

const newGroupForm = ref({
  name: '',
  type: 'select',
  proxies: [] as string[],
  exclude: ''
})

const groupTypes = ['select', 'url-test', 'fallback', 'load-balance']

const openGroupDialog = () => {
  editingGroupId.value = null
  newGroupForm.value = { name: '', type: 'select', proxies: [], exclude: '' }
  groupDialogVisible.value = true
}

const editCustomGroup = (groupName: string) => {
  const customInfo = customGroupsDict.value[groupName]
  if (!customInfo) return
  editingGroupId.value = customInfo.ID
  let proxiesList: string[] = []
  try { proxiesList = JSON.parse(customInfo.Proxies || '[]') } catch(e){}
  newGroupForm.value = {
    name: customInfo.Name || customInfo.name,
    type: customInfo.Type || customInfo.type,
    proxies: proxiesList,
    exclude: customInfo.Exclude || customInfo.exclude || ''
  }
  groupDialogVisible.value = true
}

const deleteCustomGroup = (groupName: string) => {
  const customInfo = customGroupsDict.value[groupName]
  if (!customInfo) return
  ElMessageBox.confirm(`确定要删除自定义策略组 [${groupName}] 吗？`, '安全提示', {
    confirmButtonText: '确定删除',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(async () => {
    try {
      const res = await axios.delete(`http://localhost:8080/api/custom-groups/${customInfo.ID}`)
      if (res.data.code === 200) {
        ElMessage.success('自定义策略组已成功删除！')
        await fetchCustomData()
        if (inputUrl.value) {
          await handleDecode()
        }
      }
    } catch(err: any) {
      ElMessage.error('删除失败: ' + err.message)
    }
  }).catch(() => {})
}

const selectAllNodes = () => {
  if (!newGroupForm.value.proxies.includes('[ALL_NODES]')) {
    newGroupForm.value.proxies.push('[ALL_NODES]')
  }
}

const selectAllExistingGroups = () => {
  const currentGroups = proxyGroups.value.map(g => g.name)
  for (const g of currentGroups) {
    if (!newGroupForm.value.proxies.includes(g)) {
      newGroupForm.value.proxies.push(g)
    }
  }
}

const saveCustomGroup = async () => {
  if (!newGroupForm.value.name) {
    ElMessage.warning('请输入策略组名称')
    return
  }
  if (newGroupForm.value.proxies.length === 0) {
    ElMessage.warning('请至少选择一个代理或节点')
    return
  }

  isSubmittingGroup.value = true
  try {
    let res;
    if (editingGroupId.value) {
      res = await axios.put(`http://localhost:8080/api/custom-groups/${editingGroupId.value}`, newGroupForm.value)
    } else {
      res = await axios.post('http://localhost:8080/api/custom-groups', newGroupForm.value)
    }
    if (res.data.code === 200) {
      ElMessage.success(editingGroupId.value ? '自定义组更新成功！' : '自定义组已云端保存成功！')
      groupDialogVisible.value = false
      await fetchCustomData()
      if (inputUrl.value) {
        await handleDecode()
      }
    } else {
      throw new Error(res.data.message)
    }
  } catch (error: any) {
    ElMessage.error('保存失败: ' + (error.response?.data?.message || error.message))
  } finally {
    isSubmittingGroup.value = false
  }
}

// ---------------------- 自定义节点管理逻辑 ----------------------
const nodeDialogVisible = ref(false)
const nodeActiveTab = ref('link')
const isParsingLink = ref(false)
const isSubmittingNode = ref(false)
const editingNodeId = ref<number | null>(null)

const nodeLinkForm = ref({
  link: ''
})

const newNodeForm = ref({
  name: '',
  type: 'vless',
  server: '',
  port: 443,
  config: {} as Record<string, any>
})

const nodeTypes = ['vless', 'hysteria2', 'ss', 'vmess', 'trojan', 'socks5']

const configString = computed({
  get: () => JSON.stringify(newNodeForm.value.config, null, 2),
  set: (val: string) => {
    try {
      newNodeForm.value.config = JSON.parse(val)
    } catch(e) {
      // ignore
    }
  }
})

const openNodeDialog = () => {
  editingNodeId.value = null
  nodeLinkForm.value.link = ''
  newNodeForm.value = { name: '', type: 'vless', server: '', port: 443, config: {} }
  nodeDialogVisible.value = true
  nodeActiveTab.value = 'link'
}

const editCustomNode = (nodeName: string) => {
  const customInfo = customNodesDict.value[nodeName]
  if (!customInfo) return
  editingNodeId.value = customInfo.ID
  let configMap: Record<string, any> = {}
  try { configMap = JSON.parse(customInfo.Config || '{}') } catch(e){}
  newNodeForm.value = {
    name: customInfo.Name || customInfo.name,
    type: customInfo.Type || customInfo.type,
    server: customInfo.Server || customInfo.server,
    port: customInfo.Port || customInfo.port,
    config: configMap
  }
  nodeActiveTab.value = 'manual'
  nodeDialogVisible.value = true
}

const deleteCustomNode = (nodeName: string) => {
  const customInfo = customNodesDict.value[nodeName]
  if (!customInfo) return
  ElMessageBox.confirm(`确定要彻底删除自定义节点 [${nodeName}] 吗？`, '安全提示', {
    confirmButtonText: '立即销毁',
    cancelButtonText: '取消保留',
    type: 'warning'
  }).then(async () => {
    try {
      const res = await axios.delete(`http://localhost:8080/api/custom-nodes/${customInfo.ID}`)
      if (res.data.code === 200) {
        ElMessage.success('自定义节点已被彻底删除！')
        await fetchCustomData()
        if (inputUrl.value) {
          await handleDecode()
        }
      }
    } catch(err: any) {
      ElMessage.error('删除节点失败: ' + err.message)
    }
  }).catch(() => {})
}

const parseNodeLink = async () => {
  if (!nodeLinkForm.value.link) {
    ElMessage.warning('请输入节点链接')
    return
  }
  isParsingLink.value = true
  try {
    const res = await axios.post('http://localhost:8080/api/parse-link', { link: nodeLinkForm.value.link })
    if (res.data.code === 200) {
      const data = res.data.data
      newNodeForm.value.name = data.name || ''
      newNodeForm.value.type = data.type || 'vless'
      newNodeForm.value.server = data.server || ''
      newNodeForm.value.port = data.port || 443
      newNodeForm.value.config = data.config || {}
      ElMessage.success('链接解析成功！请在右侧检查参数')
      nodeActiveTab.value = 'manual'
    } else {
      throw new Error(res.data.message)
    }
  } catch (error: any) {
    ElMessage.error('解析失败: ' + (error.response?.data?.message || error.message))
  } finally {
    isParsingLink.value = false
  }
}

const saveCustomNode = async () => {
  if (!newNodeForm.value.name || !newNodeForm.value.server || !newNodeForm.value.port) {
    ElMessage.warning('请补全基础信息（名称、服务器、端口）')
    return
  }
  isSubmittingNode.value = true
  
  // 同步基础信息到 config
  newNodeForm.value.config.name = newNodeForm.value.name
  newNodeForm.value.config.type = newNodeForm.value.type
  newNodeForm.value.config.server = newNodeForm.value.server
  newNodeForm.value.config.port = newNodeForm.value.port

  try {
    let res;
    if (editingNodeId.value) {
      res = await axios.put(`http://localhost:8080/api/custom-nodes/${editingNodeId.value}`, newNodeForm.value)
    } else {
      res = await axios.post('http://localhost:8080/api/custom-nodes', newNodeForm.value)
    }
    if (res.data.code === 200) {
      ElMessage.success(editingNodeId.value ? '自定义节点更新成功！' : '自定义节点云端保存成功！')
      nodeDialogVisible.value = false
      await fetchCustomData()
      if (inputUrl.value) {
        await handleDecode()
      }
    } else {
      throw new Error(res.data.message)
    }
  } catch (error: any) {
    ElMessage.error('保存失败: ' + (error.response?.data?.message || error.message))
  } finally {
    isSubmittingNode.value = false
  }
}
</script>

<template>
  <div class="app-container">
    <!-- 头部精致毛玻璃导航栏 -->
    <header class="main-header glass-card">
      <div class="logo-wrapper">
        <span class="logo-icon">⚡</span>
        <h1 class="logo-title text-gradient">CLASH SUBSCRIPTION DECODER</h1>
      </div>
      <div class="header-actions">
        <el-tag size="large" type="primary" effect="dark" round class="status-indicator">
          <span class="pulse-dot"></span>后端就绪 (:8080)
        </el-tag>
      </div>
    </header>

    <!-- 中部内容主体区 -->
    <main class="main-content">
      <!-- 控制卡片面板 -->
      <section class="control-panel glass-card">
        <h2 class="section-title">自适应 Base64 地址获取器</h2>
        <p class="section-desc">输入任意提供 Base64 编码数据的订阅地址或接口 URL，后端将自动请求、清洗并进行多重自适应解码。</p>
        
        <div class="input-area">
          <el-input
            v-model="inputUrl"
            placeholder="请输入订阅地址 URL (例如: https://example.com/sub)..."
            clearable
            @clear="handleClear"
            @keyup.enter="handleDecode"
            :disabled="isLoading"
            class="decode-input"
          >
            <template #prefix>
              <el-icon class="input-prefix-icon"><Link /></el-icon>
            </template>
          </el-input>
          
          <div class="button-group">
            <el-button
              type="primary"
              @click="handleDecode"
              :loading="isLoading"
              class="action-btn"
            >
              一键抓取并解码
            </el-button>
            <el-button 
              type="info" 
              plain 
              @click="handleQuickMock" 
              :disabled="isLoading"
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
                <el-tag size="small" effect="plain" type="info">{{ (stats.size / 1024).toFixed(2) }} KB</el-tag>
              </div>
              <div class="meta-item">
                <span class="meta-label">总行数：</span>
                <el-tag size="small" effect="plain" type="info">{{ stats.lines }} 行</el-tag>
              </div>
              <div v-if="parsedNodes.length > 0" class="meta-item">
                <span class="meta-label">检测到节点：</span>
                <el-tag size="small" effect="dark" type="success">{{ parsedNodes.length }} 个</el-tag>
              </div>
            </div>
            
            <div class="result-actions">
              <el-button-group>
                <el-button type="success" size="default" icon="CopyDocument" @click="handleCopy">
                  复制明文
                </el-button>
                <el-button type="primary" size="default" icon="Download" @click="handleDownload">
                  导出 YAML
                </el-button>
              </el-button-group>
            </div>
          </div>

          <!-- 页签切换区 -->
          <el-tabs v-model="activeTab" class="custom-tabs">
            <!-- 节点预览页签 -->
            <el-tab-pane name="nodes" v-if="parsedNodes.length > 0">
              <template #label>
                <span class="tab-label">⚡ 节点解析概览 ({{ parsedNodes.length }})</span>
              </template>
              <div class="nodes-header-actions" style="display:flex; justify-content:flex-end; margin-bottom: 16px;">
                <el-button type="success" effect="dark" round @click="openNodeDialog">
                  <span style="margin-right: 4px; font-weight: bold;">+</span> ✨ 新增自定义节点
                </el-button>
              </div>
              <div class="nodes-grid">
                <div v-for="(node, idx) in parsedNodes" :key="idx" class="node-card">
                  <div class="node-card-header">
                    <div style="display: flex; align-items: center; overflow: hidden; flex: 1;">
                      <span class="node-flag">{{ getFlagEmoji(node.name) }}</span>
                      <span class="node-name" :title="node.name">{{ node.name }}</span>
                    </div>
                    <div v-if="customNodesDict[node.name]" class="custom-actions" style="display: flex; gap: 4px;">
                      <el-button type="primary" link :icon="Edit" @click="editCustomNode(node.name)" title="编辑云端节点"></el-button>
                      <el-button type="danger" link :icon="Delete" @click="deleteCustomNode(node.name)" title="删除云端节点"></el-button>
                    </div>
                  </div>
                  <div class="node-card-body">
                    <div class="node-info-row">
                      <span class="info-label">地址:</span>
                      <span class="info-val" :title="node.server">{{ node.server }}</span>
                    </div>
                    <div class="node-info-row">
                      <span class="info-label">端口:</span>
                      <span class="info-val highlight-port">{{ node.port }}</span>
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
                    <span v-if="node.details.cipher" class="cipher-label">{{ node.details.cipher }}</span>
                  </div>
                </div>
              </div>
            </el-tab-pane>

            <!-- 代理组页签 -->
            <el-tab-pane name="groups" v-if="proxyGroups.length > 0">
              <template #label>
                <span class="tab-label">🗂️ 代理组策略 ({{ proxyGroups.length }})</span>
              </template>
              
              <div class="groups-header-actions" style="display:flex; justify-content:flex-end; margin-bottom: 16px;">
                <el-button type="primary" effect="dark" round @click="openGroupDialog">
                  <span style="margin-right: 4px; font-weight: bold;">+</span> 新增自定义策略组
                </el-button>
              </div>

              <div class="groups-grid">
                <div v-for="(group, idx) in proxyGroups" :key="idx" class="group-card">
                  <div class="group-card-header">
                    <div style="display: flex; align-items: center; overflow: hidden; flex: 1; gap: 8px;">
                      <span class="group-name">{{ group.name }}</span>
                      <el-tag size="small" type="primary" effect="dark" class="group-type-tag">{{ group.type }}</el-tag>
                    </div>
                    <div v-if="customGroupsDict[group.name]" class="custom-actions" style="display: flex; gap: 4px;">
                      <el-button type="primary" link :icon="Edit" @click="editCustomGroup(group.name)" title="编辑策略组"></el-button>
                      <el-button type="danger" link :icon="Delete" @click="deleteCustomGroup(group.name)" title="删除策略组"></el-button>
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
                <span class="tab-label">📋 分流规则 ({{ parsedConfig.rules.length }})</span>
              </template>
              
              <div class="rules-container glass-card">
                <div class="rules-toolbar">
                  <el-input
                    v-model="ruleSearchQuery"
                    placeholder="输入关键字检索规则类型、内容或策略..."
                    clearable
                    class="rule-search-input"
                  >
                    <template #prefix>
                      <span style="font-size: 16px;">🔍</span>
                    </template>
                  </el-input>
                </div>
                
                <el-table :data="paginatedRules" height="500" class="custom-table" style="width: 100%">
                  <el-table-column prop="type" label="规则类型" width="160">
                    <template #default="scope">
                      <el-tag size="small" effect="dark" type="warning" class="rule-type-tag">{{ scope.row.type }}</el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column prop="payload" label="匹配内容" show-overflow-tooltip>
                    <template #default="scope">
                      <span class="rule-payload">{{ scope.row.payload }}</span>
                    </template>
                  </el-table-column>
                  <el-table-column prop="target" label="目标策略" width="220">
                    <template #default="scope">
                      <el-tag size="small" effect="plain" type="success" class="rule-target-tag">{{ scope.row.target }}</el-tag>
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
                  :style="{ minHeight: '400px', maxHeight: '600px', fontSize: '14px', borderRadius: '12px' }"
                  :indent-with-tab="true"
                  :tab-size="2"
                />
              </div>
            </el-tab-pane>

            <!-- 原始 Base64 页签 -->
            <el-tab-pane name="raw">
              <template #label>
                <span class="tab-label">🔗 原始 Base64 截断</span>
              </template>
              <div class="code-wrapper">
                <div class="code-container raw-base64-text">{{ result.raw_base64 }}</div>
              </div>
            </el-tab-pane>
          </el-tabs>
        </section>
      </transition>
    </main>

    <!-- 自定义策略组弹窗 -->
    <el-dialog 
      v-model="groupDialogVisible" 
      :title="editingGroupId ? '✨ 编辑云端自定义策略组' : '✨ 新增云端自定义策略组'" 
      width="550px" 
      class="glass-dialog"
    >
      <el-form label-position="top">
        <el-form-item label="策略组名称">
          <el-input v-model="newGroupForm.name" placeholder="例如：我的超强备用线路"></el-input>
        </el-form-item>
        <el-form-item label="策略类型 (Type)">
          <el-select v-model="newGroupForm.type" style="width: 100%">
            <el-option v-for="t in groupTypes" :key="t" :label="t.toUpperCase()" :value="t" />
          </el-select>
        </el-form-item>
        <el-form-item label="配置包含的目标代理">
          <div style="margin-bottom: 12px; display: flex; gap: 10px;">
            <el-button size="small" type="primary" plain @click="selectAllNodes">
              注入全部最新节点 [ALL_NODES]
            </el-button>
            <el-button size="small" type="info" plain @click="selectAllExistingGroups">
              引入所有现有策略组
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
            <el-option label="🌟 动态注入当前订阅全节点 [ALL_NODES]" value="[ALL_NODES]" />
            <el-option-group label="现有策略组">
              <el-option v-for="g in proxyGroups" :key="g.name" :label="g.name" :value="g.name" />
            </el-option-group>
            <el-option-group label="现有独立节点">
              <el-option v-for="n in parsedNodes" :key="n.name" :label="n.name" :value="n.name" />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item label="排除节点关键字或正则 (Exclude) - 极简可选">
          <el-input v-model="newGroupForm.exclude" placeholder="例如：特殊专线" clearable></el-input>
          <p style="font-size: 12px; color: var(--text-secondary); margin-top: 4px; line-height: 1.4;">
            仅当您有特殊的跨组排除需求时（如特定节点总是断流），才在此处填入正则表达式。
          </p>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="groupDialogVisible = false" plain>取消</el-button>
        <el-button type="primary" @click="saveCustomGroup" :loading="isSubmittingGroup">
          {{ editingGroupId ? '更新并立即云端同步' : '保存并立即云端同步' }}
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
          <div style="padding: 10px 0;">
            <p style="color: var(--text-secondary); margin-bottom: 15px; font-size: 14px;">
              支持自动解析 vless://, hysteria2://, ss://, socks5:// 等分享链接，一键提取核心参数。
            </p>
            <el-input 
              v-model="nodeLinkForm.link" 
              type="textarea" 
              :rows="4" 
              placeholder="请粘贴您的节点链接..."
            ></el-input>
            <div style="margin-top: 15px; text-align: right;">
              <el-button type="primary" @click="parseNodeLink" :loading="isParsingLink">一键解析链接</el-button>
            </div>
          </div>
        </el-tab-pane>

        <el-tab-pane label="✍️ 手动配置调整" name="manual">
          <el-form label-position="left" label-width="80px" style="padding: 10px 0; max-height: 400px; overflow-y: auto;">
            <el-form-item label="节点名称">
              <el-input v-model="newNodeForm.name" placeholder="请输入节点显示的名称"></el-input>
            </el-form-item>
            <el-form-item label="协议类型">
              <el-select v-model="newNodeForm.type" style="width: 100%">
                <el-option v-for="t in nodeTypes" :key="t" :label="t.toUpperCase()" :value="t" />
              </el-select>
            </el-form-item>
            <el-form-item label="服务器">
              <el-input v-model="newNodeForm.server" placeholder="例如：example.com 或 IP"></el-input>
            </el-form-item>
            <el-form-item label="端口">
              <el-input-number v-model="newNodeForm.port" :min="1" :max="65535" style="width: 100%"></el-input-number>
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
                  <el-option v-for="g in proxyGroups" :key="g.name" :label="g.name" :value="g.name" />
                </el-option-group>
                <el-option-group label="现有独立节点">
                  <el-option v-for="n in parsedNodes" :key="n.name" :label="n.name" :value="n.name" />
                </el-option-group>
              </el-select>
              <p style="font-size: 12px; color: var(--text-secondary); margin-top: 4px; line-height: 1.4;">
                最新内核移除了 relay，链式代理现由前置拨号 (dialer-proxy) 原生接管。
              </p>
            </el-form-item>
            <el-form-item label="详细配置">
              <p style="font-size: 12px; color: var(--text-secondary); margin-top: 0; line-height: 1.4;">
                高级参数（如 uuid, tls, network 等），将作为 JSON 对象合并到该节点配置中。<br/>
                解析链接后，这里会预先填充。如需手动输入格式请使用合法的 JSON。
              </p>
              <codemirror
                v-model="configString"
                :extensions="cmExtensions"
                :style="{ width: '100%', maxHeight: '200px', fontSize: '13px', borderRadius: '8px' }"
                :indent-with-tab="true"
                :tab-size="2"
              />
            </el-form-item>
          </el-form>
          <div style="margin-top: 20px; text-align: right;">
            <el-button @click="nodeDialogVisible = false" plain>取消</el-button>
            <el-button type="success" @click="saveCustomNode" :loading="isSubmittingNode">
              {{ editingNodeId ? '确认并更新云端' : '确认并存入云端' }}
            </el-button>
          </div>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <!-- 页脚版权说明 -->
    <footer class="main-footer">
      <p>Base64 Subscription Analyzer & Decoder © 2026. Built with Gin & Vue 3 + Element Plus.</p>
    </footer>
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
  0% { transform: translateY(0) scale(1); }
  100% { transform: translateY(-4px) scale(1.1); }
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

.control-panel {
  padding: 30px;
  text-align: left;
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

.action-btn {
  padding: 20px 28px !important;
  font-size: 15px !important;
}

.mock-btn {
  border-radius: 12px !important;
  padding: 20px 24px !important;
  font-size: 14px !important;
  background-color: rgba(255,255,255,0.02) !important;
  border-color: rgba(255,255,255,0.08) !important;
  color: var(--text-secondary) !important;
  transition: all 0.3s !important;
}

.mock-btn:hover {
  background-color: rgba(255,255,255,0.06) !important;
  color: var(--text-primary) !important;
  border-color: rgba(255,255,255,0.18) !important;
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

.node-card-header {
  display: flex;
  align-items: center;
  gap: 8px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.03);
  padding-bottom: 8px;
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
  background: rgba(255,255,255,0.03);
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
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from, .fade-leave-to {
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
  box-shadow: inset 0 2px 10px rgba(0, 0, 0, 0.5), 0 4px 15px rgba(0, 0, 0, 0.2);
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

.editor-glass-wrapper :deep(.cm-activeLine), .editor-glass-wrapper :deep(.cm-activeLineGutter) {
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
</style>
