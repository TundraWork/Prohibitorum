import{J as u,d as f,Y as y,e as r,P as v,g as i,am as h,l as g,h as k,t as x,v as z,f as C,x as b,y as U,c as l,o as t}from"./index-CKIUwGwl.js";/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const B=u("LoaderCircleIcon",[["path",{d:"M21 12a9 9 0 1 1-6.219-8.56",key:"13zald"}]]);/**
 * @license lucide-vue-next v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const N=u("UserIcon",[["path",{d:"M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2",key:"975kel"}],["circle",{cx:"12",cy:"7",r:"4",key:"17ys0d"}]]),_=["src"],w={key:3,"data-test":"avatar-spinner",class:"absolute inset-0 flex items-center justify-center bg-black/40"},j=f({__name:"UserAvatar",props:{displayName:{},username:{},size:{default:"md"},src:{},loading:{type:Boolean}},setup(n){const s=n,c=U(!1);y(()=>s.src,()=>{c.value=!1});const m=l(()=>!!s.src&&!c.value),d=l(()=>{const o=(s.displayName??"").trim();if(o){const e=o.split(/\s+/).filter(Boolean);return(e.length>=2?e[0][0]+e[e.length-1][0]:e[0].slice(0,2)).toUpperCase()}const a=(s.username??"").trim();return a?a.slice(0,2).toUpperCase():""}),p=l(()=>s.size==="sm"?"size-6 text-[0.625rem]":"size-8 text-xs");return(o,a)=>(t(),r("span",{"aria-hidden":"true",class:v(i(h)("relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-sidebar-accent font-medium text-sidebar-accent-foreground",p.value))},[m.value?(t(),r("img",{key:0,src:n.src,alt:"",loading:"lazy",class:"size-full object-cover",onError:a[0]||(a[0]=e=>c.value=!0)},null,40,_)):d.value?(t(),r(g,{key:1},[k(x(d.value),1)],64)):(t(),z(i(N),{key:2,class:"size-4"})),n.loading?(t(),r("span",w,[C(i(B),{class:"size-3 animate-spin text-white"})])):b("",!0)],2))}});export{B as L,N as U,j as _};
