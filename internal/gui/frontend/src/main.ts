import "./styles/tokens.css";
import "./styles/fonts.css";
import "./styles/base.css";
// KaTeX CSS is NOT eager here — Markdown.svelte dynamic-imports it the first
// time math actually renders, keeping ~240KB of math styling off the
// render-blocking cold-start path.
import { mount } from "svelte";
import App from "./App.svelte";

const target = document.getElementById("app");
if (!target) throw new Error("#app mount target missing");

export default mount(App, { target });
