"use strict";
(() => {
 const $=id=>document.getElementById(id);
 const send=(action,payload={})=>window.ipc.postMessage(JSON.stringify({action,...payload}));
 const toggles=[["gesture-enabled","altEnabled"],["alt-enabled","altEnabled"],["hotkey-enabled","hotkeyEnabled"],["auto-speak","autoSpeak"],["startup-enabled","startupEnabled"]];
 let snapshot,dirty=false,accountHovered=false,selectedModel="";
 function accountLabel(){const connected=!!snapshot?.auth?.connected;$("auth-badge").textContent=connected?(accountHovered?"断开":"已连接"):"连接"}
 function render(){
  if(!snapshot)return;
  const auth=snapshot.auth||{},busy=snapshot.authBusy;
  $("account-name").textContent=auth.email||"";
  accountLabel();
  $("auth-badge").classList.toggle("connected",!!auth.connected);
  $("auth-badge").title=snapshot.authMessage||(auth.connected?"已连接":"连接 OpenAI 账号");
  $("auth-badge").disabled=busy;
  $("refresh-models").disabled=!auth.connected||busy||snapshot.modelsBusy;
  $("refresh-models").title=snapshot.modelsMessage||"刷新模型列表";
  $("refresh-models").setAttribute("aria-busy",String(!!snapshot.modelsBusy));
  const model=$("model"),previous=model.value||selectedModel,models=auth.models||[];
  model.disabled=!!snapshot.modelsBusy;
  $("save-settings").disabled=!!snapshot.modelsBusy;
  model.replaceChildren();
  if(snapshot.modelsBusy){const option=document.createElement("option");option.value="";option.textContent="更新中";model.append(option)}
  else if(!models.length){const option=document.createElement("option");option.value="";option.textContent="请先连接账号";model.append(option)}
  else{for(const item of models){const option=document.createElement("option");option.value=item.id;option.textContent=item.label||item.id.replace("chatgpt:","");model.append(option)}model.value=dirty&&models.some(m=>m.id===previous)?previous:snapshot.config.model;if(!model.value)model.selectedIndex=0}
  if(!snapshot.modelsBusy)selectedModel=model.value;
  if(!dirty)for(const [id,key] of toggles)$(id).checked=snapshot.config[key];
  requestAnimationFrame(()=>{const dialog=$("settings-dialog"),layout=document.querySelector(".settings-layout"),style=getComputedStyle(dialog);const height=Math.ceil(layout.getBoundingClientRect().height+parseFloat(style.paddingTop)+parseFloat(style.paddingBottom)+2);if(height>dialog.clientHeight)send("settings-height",{height})});
 }
 window.touchdict={applyState(value){snapshot=value;render()},openSettings(){dirty=false;accountHovered=false;render();$("settings-dialog").focus({preventScroll:true})},settingsSaved(){dirty=false;send("close")},notice(message){$("auth-badge").title=message}};
 $("settings-dialog").show();$("settings-dialog").focus({preventScroll:true});$("settings-dialog").addEventListener("change",()=>dirty=true);
 $("cancel-settings").onclick=()=>send("close");
 // The existing Windows three-finger gesture is configured to emit left Alt.
 $("gesture-enabled").onchange=()=>{$("alt-enabled").checked=$("gesture-enabled").checked};
 $("alt-enabled").onchange=()=>{$("gesture-enabled").checked=$("alt-enabled").checked};
 $("auth-badge").onmouseenter=()=>{accountHovered=true;accountLabel()};$("auth-badge").onmouseleave=()=>{accountHovered=false;accountLabel()};
 $("auth-badge").onclick=()=>{if(snapshot&&!snapshot.authBusy)send(snapshot.auth?.connected?"disconnect":"authorize")};$("refresh-models").onclick=()=>{if(!snapshot||snapshot.modelsBusy)return;selectedModel=$("model").value;snapshot.modelsBusy=true;render();send("refresh-models")};
 $("save-settings").onclick=()=>{if(!snapshot)return;const config={...snapshot.config,model:$("model").value};for(const [id,key] of toggles)config[key]=$(id).checked;send("save-settings",{config})};
 document.addEventListener("keydown",event=>{if(event.key==="Escape")send("close")});document.addEventListener("contextmenu",event=>event.preventDefault());send("ready");
})();
