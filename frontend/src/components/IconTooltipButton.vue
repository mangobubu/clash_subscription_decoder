<script setup lang="ts">
import type { Component } from "vue";

withDefaults(
  defineProps<{
    label: string;
    icon: string | Component;
    type?: "primary" | "success" | "warning" | "danger" | "info" | "default";
    size?: "large" | "default" | "small";
    placement?: "top" | "bottom" | "left" | "right";
    disabled?: boolean;
    loading?: boolean;
    plain?: boolean;
    text?: boolean;
    link?: boolean;
    circle?: boolean;
    round?: boolean;
    tooltipDisabled?: boolean;
    ariaLabel?: string;
  }>(),
  {
    type: "default",
    size: "default",
    placement: "top",
    disabled: false,
    loading: false,
    plain: false,
    text: false,
    link: false,
    circle: true,
    round: false,
    tooltipDisabled: false,
    ariaLabel: "",
  },
);

const emit = defineEmits<{
  click: [event: MouseEvent];
}>();

const handleClick = (event: MouseEvent) => {
  emit("click", event);
};
</script>

<template>
  <el-tooltip
    :content="label"
    :placement="placement"
    :disabled="tooltipDisabled || disabled"
    effect="dark"
    popper-class="app-tooltip"
  >
    <el-button
      class="icon-tooltip-button"
      :aria-label="ariaLabel || label"
      :circle="circle"
      :disabled="disabled"
      :icon="icon"
      :link="link"
      :loading="loading"
      :plain="plain"
      :round="round"
      :size="size"
      :text="text"
      :type="type === 'default' ? undefined : type"
      @click="handleClick"
    >
      <span v-if="$slots.default" class="icon-tooltip-button__label">
        <slot />
      </span>
    </el-button>
  </el-tooltip>
</template>

<style scoped>
.icon-tooltip-button {
  display: inline-flex !important;
  align-items: center !important;
  justify-content: center !important;
  gap: 6px;
  flex: 0 0 auto;
  line-height: 1 !important;
}

.icon-tooltip-button:not(.is-link):not(.is-text) {
  min-width: 40px;
  min-height: 40px;
}

.icon-tooltip-button.is-circle {
  width: 40px;
  height: 40px;
  padding: 0 !important;
}

.icon-tooltip-button :deep(.el-icon) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: 20px;
  height: 20px;
  margin: 0;
  font-size: 20px;
  line-height: 1;
}

.icon-tooltip-button :deep(.el-icon svg) {
  display: block;
  width: 20px;
  height: 20px;
  overflow: visible;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 28;
  paint-order: stroke fill;
}

.icon-tooltip-button__label {
  margin-left: 0;
  line-height: 1;
}

@media (max-width: 640px) {
  .icon-tooltip-button:not(.is-link):not(.is-text) {
    min-width: 42px;
    min-height: 42px;
  }

  .icon-tooltip-button.is-circle {
    width: 42px;
    height: 42px;
  }

  .icon-tooltip-button :deep(.el-icon),
  .icon-tooltip-button :deep(.el-icon svg) {
    width: 21px;
    height: 21px;
    font-size: 21px;
  }
}
</style>
