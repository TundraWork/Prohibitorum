import{J as i,d as _,a as k,v as u,g as a,aN as y,aO as v,w as t,f as s,aP as S,P as d,aQ as L,aR as C,e as V,l as b,D as w,c as x,o as n,aS as z,h as B,t as I}from"./index-BYP-PFe3.js";/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const M=i("LanguagesIcon",[["path",{d:"m5 8 6 6",key:"1wu5hv"}],["path",{d:"m4 14 6-6 2-3",key:"1k1g8d"}],["path",{d:"M2 5h12",key:"or177f"}],["path",{d:"M7 2h1",key:"1t2jsx"}],["path",{d:"m22 22-5-10-5 10",key:"don7ne"}],["path",{d:"M14 18h6",key:"1m8k6r"}]]);/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const E=i("ShieldCheckIcon",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}],["path",{d:"m9 12 2 2 4-4",key:"dzmm74"}]]),N=_({__name:"LocaleSwitcher",props:{largeTarget:{type:Boolean,default:!1}},setup(m){const c=m,{t:h,locale:o,availableLocales:p}=k({useScope:"global"}),g={en:"English",zh:"中文"},f=x(()=>p.map(l=>({value:l,label:g[l]??l})));return(l,r)=>(n(),u(a(y),{modelValue:a(o),"onUpdate:modelValue":r[0]||(r[0]=e=>v(o)?o.value=e:null)},{default:t(()=>[s(a(S),{class:d(["w-fit gap-1.5",c.largeTarget?"h-11 min-w-11":"h-8"]),"aria-label":a(h)("common.language"),"data-test":"locale-trigger"},{default:t(()=>[s(a(M),{class:"size-4 text-muted","aria-hidden":"true"}),s(a(L))]),_:1},8,["class","aria-label"]),s(a(C),{align:"start"},{default:t(()=>[(n(!0),V(b,null,w(f.value,e=>(n(),u(a(z),{key:e.value,value:e.value,class:d(c.largeTarget?"min-h-11":void 0),"data-test":"locale-option"},{default:t(()=>[B(I(e.label),1)]),_:2},1032,["value","class"]))),128))]),_:1})]),_:1},8,["modelValue"]))}});export{E as S,N as _};
