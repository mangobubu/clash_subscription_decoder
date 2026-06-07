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
  flex: 0 0 auto;
}

.icon-tooltip-button:not(.is-link):not(.is-text) {
  min-width: 40px;
  min-height: 40px;
}

.icon-tooltip-button__label {
  margin-left: 6px;
}

@media (max-width: 640px) {
  .icon-tooltip-button:not(.is-link):not(.is-text) {
    min-width: 42px;
    min-height: 42px;
  }
}
</style>
