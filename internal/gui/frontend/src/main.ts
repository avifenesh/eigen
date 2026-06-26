import "./styles/tokens.css";
import "./styles/fonts.css";
import "./styles/base.css";
import { mount } from "svelte";
import App from "./App.svelte";

const target = document.getElementById("app");
if (!target) throw new Error("#app mount target missing");

export default mount(App, { target });
