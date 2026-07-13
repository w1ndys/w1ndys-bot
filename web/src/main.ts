// 📌 影响范围：读取浏览器 DOM；挂载 Vue 根组件和路由器。
import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './styles.css'

createApp(App).use(router).mount('#app')
