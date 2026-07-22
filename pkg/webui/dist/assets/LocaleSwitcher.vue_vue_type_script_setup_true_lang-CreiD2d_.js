import{B as i,d as _,a as k,l as u,f as a,a3 as y,w as l,e as s,G as d,b as v,F as S,v as b,c as L,o as n,g as C,t as V}from"./index-BL3Uvr3T.js";import{S as w,_ as x,a as B,b as z,c as I}from"./SelectItem.vue_vue_type_script_setup_true_lang-DRDDUh47.js";/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const M=i("LanguagesIcon",[["path",{d:"m5 8 6 6",key:"1wu5hv"}],["path",{d:"m4 14 6-6 2-3",key:"1k1g8d"}],["path",{d:"M2 5h12",key:"or177f"}],["path",{d:"M7 2h1",key:"1t2jsx"}],["path",{d:"m22 22-5-10-5 10",key:"don7ne"}],["path",{d:"M14 18h6",key:"1m8k6r"}]]);/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const $=i("ShieldCheckIcon",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}],["path",{d:"m9 12 2 2 4-4",key:"dzmm74"}]]),F=_({__name:"LocaleSwitcher",props:{largeTarget:{type:Boolean,default:!1}},setup(m){const c=m,{t:p,locale:o,availableLocales:h}=k({useScope:"global"}),g={en:"English",zh:"中文"},f=L(()=>h.map(t=>({value:t,label:g[t]??t})));return(t,r)=>(n(),u(a(w),{modelValue:a(o),"onUpdate:modelValue":r[0]||(r[0]=e=>y(o)?o.value=e:null)},{default:l(()=>[s(a(x),{class:d(["w-fit gap-1.5",c.largeTarget?"h-11 min-w-11":"h-8"]),"aria-label":a(p)("common.language"),"data-test":"locale-trigger"},{default:l(()=>[s(a(M),{class:"size-4 text-muted","aria-hidden":"true"}),s(a(B))]),_:1},8,["class","aria-label"]),s(a(z),{align:"start"},{default:l(()=>[(n(!0),v(S,null,b(f.value,e=>(n(),u(a(I),{key:e.value,value:e.value,class:d(c.largeTarget?"min-h-11":void 0),"data-test":"locale-option"},{default:l(()=>[C(V(e.label),1)]),_:2},1032,["value","class"]))),128))]),_:1})]),_:1},8,["modelValue"]))}});export{$ as S,F as _};
