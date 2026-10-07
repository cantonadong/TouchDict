"use strict";
(() => {
 const $=id=>document.getElementById(id);
 const send=(action,payload={})=>window.chrome.webview.postMessage(JSON.stringify({action,...payload}));
 const text=(id,value)=>{const content=value||"";if($(id).textContent!==content)$(id).textContent=content};
 const show=(id,value)=>{$(id).hidden=!value};
 let snapshot=null,lastHeight=-1,frame=0,toastTimer;
 function measure(){cancelAnimationFrame(frame);frame=requestAnimationFrame(()=>{const content=$("result-content"),footer=document.querySelector(".result-footer");const height=Math.ceil(content.scrollHeight+footer.offsetHeight+48);if(height!==lastHeight){lastHeight=height;send("content-height",{height})}})}
 function context(sentence,query){const node=$("context-sentence"),signature=JSON.stringify([sentence,query]);show("context-card",!!sentence);if(node.dataset.signature===signature)return;node.dataset.signature=signature;node.replaceChildren();const lower=sentence.toLowerCase(),needle=query.trim().toLowerCase();let offset=0,index;while(needle&&(index=lower.indexOf(needle,offset))!==-1){node.append(document.createTextNode(sentence.slice(offset,index)));const mark=document.createElement("mark");mark.textContent=sentence.slice(index,index+needle.length);node.append(mark);offset=index+needle.length}node.append(document.createTextNode(sentence.slice(offset)))}
 window.touchdict={
  applyState(data){snapshot=data;const state=data.state,kind=state.Kind,d=state.Definition||{},empty=kind===0||kind===3;show("welcome",empty);show("definition",!empty);text("welcome-message",state.Message);text("term",kind===2?d.term:state.Selection);text("pos",d.partOfSpeech);show("pos",kind===2&&!!d.partOfSpeech);context(state.Context||"",state.Selection||d.term||"");show("loading",kind===1);text("meaning",d.meaningZh);show("meaning-card",kind===2&&!!d.meaningZh);text("example",d.exampleEn);text("translation",d.exampleZh);show("example-card",kind===2&&!!(d.exampleEn||d.exampleZh));show("translation",kind===2&&!!d.exampleZh);show("copy",kind===2&&!!d.exampleEn);text("error",state.Message);show("error",kind===4);text("status",state.Message);show("status",kind===2&&!!state.Message);show("suggestion-card",kind===5);const suggestions=$("suggestions");suggestions.replaceChildren();for(const value of state.Suggestions||[]){const button=document.createElement("button");button.textContent=value;button.onclick=()=>send("lookup",{text:value});suggestions.append(button)}$("speak").disabled=kind!==2;$("retry").disabled=!((state.Selection||d.term||"").trim());$("pin").setAttribute("aria-pressed",String(data.pinned));text("pin-text",data.pinned?"\u53d6\u6d88\u56fa\u9876":"\u56fa\u9876");document.documentElement.style.setProperty("--term-size",`${data.termSize}pt`);document.documentElement.style.setProperty("--content-size",`${data.contentSize}pt`);measure()},
  beginEdit(){show("term-edit",true);show("term",false);$("term-edit").value=$("term").textContent;$("term-edit").focus();$("term-edit").select();measure()},
  endEdit(){show("term-edit",false);show("term",true);measure()},
  notice(){clearTimeout(toastTimer);show("toast",true);toastTimer=setTimeout(()=>show("toast",false),2000)}
 };
 $("term").addEventListener("dblclick",()=>send("edit"));
 $("term-edit").addEventListener("keydown",event=>{if(event.isComposing||event.keyCode===229)return;if(event.key==="Enter"){event.preventDefault();if(event.target.value.trim())send("lookup",{text:event.target.value})}if(event.key==="Escape")send("edit-cancel")});
 $("term-edit").addEventListener("blur",()=>send("edit-cancel"));
 for(const id of ["speak","retry","pin"])$(id).addEventListener("click",()=>send(id));
 $("copy").addEventListener("click",()=>send("copy-example"));
 document.addEventListener("contextmenu",event=>event.preventDefault());
 new ResizeObserver(measure).observe($("result-content"));window.addEventListener("resize",measure);send("ready");
})();
