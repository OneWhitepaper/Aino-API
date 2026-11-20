import { createApp } from 'vue'
import App from './App.vue'
import './style.css'

createApp(App).mount('#app')
// Original skill motion runtime; loaded after Vue has mounted its controls.
void import('./assets/motion/motion.js')
