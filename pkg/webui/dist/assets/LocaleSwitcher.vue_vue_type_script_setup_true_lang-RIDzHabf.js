import{c as d}from"./createLucideIcon-D6nmhzFQ.js";import{d as m,a as g,b as n,e as k,f as s,h as c,t as i,Z as f,a1 as _,O as b,F as y,s as v,c as x,o}from"./index-bDEeShnh.js";/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const L=d("LanguagesIcon",[["path",{d:"m5 8 6 6",key:"1wu5hv"}],["path",{d:"m4 14 6-6 2-3",key:"1k1g8d"}],["path",{d:"M2 5h12",key:"or177f"}],["path",{d:"M7 2h1",key:"1t2jsx"}],["path",{d:"m22 22-5-10-5 10",key:"don7ne"}],["path",{d:"M14 18h6",key:"1m8k6r"}]]);/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const V=d("ShieldCheckIcon",[["path",{d:"M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z",key:"oel41y"}],["path",{d:"m9 12 2 2 4-4",key:"dzmm74"}]]),S={class:"inline-flex items-center gap-1.5 rounded-md border border-border bg-surface px-2 py-1 text-sm text-ink shadow-sm focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-0"},w={class:"sr-only"},C=["aria-label"],M=["value"],E=m({__name:"LocaleSwitcher",setup(z){const{t:l,locale:t,availableLocales:u}=g({useScope:"global"}),p={en:"English",zh:"中文"},h=x(()=>u.map(a=>({value:a,label:p[a]??a})));return(a,r)=>(o(),n("label",S,[k(s(L),{class:"size-4 text-muted","aria-hidden":"true"}),c("span",w,i(s(l)("common.language")),1),f(c("select",{"onUpdate:modelValue":r[0]||(r[0]=e=>b(t)?t.value=e:null),"aria-label":s(l)("common.language"),class:"cursor-pointer appearance-none bg-transparent pr-1 outline-none"},[(o(!0),n(y,null,v(h.value,e=>(o(),n("option",{key:e.value,value:e.value},i(e.label),9,M))),128))],8,C),[[_,s(t)]])]))}});export{V as S,E as _};
