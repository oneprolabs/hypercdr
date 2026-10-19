import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import type { HyperCDRFrontendModule } from "./app/extensions";
import { Building2, Users, KeyRound, Globe } from 'lucide-react';
import CommunityMigrationAuthorizationsPage from './features/migrations/community-migration-authorizations-page';
import "./styles.css";
import { PublicAuthPage, SaaSLoginSupplement } from './features/saas/saas-auth';
import { PersonalInformation, OrganizationInformation } from './features/saas/organization-settings';
import { AccountAccess } from './features/saas/account-access';
import { AccessLoginPage } from './features/saas/access-login-page';
import { MembersPage } from './features/saas/members-page';

const modules:HyperCDRFrontendModule[]=[
{id:'platform-access',view:'extension:platform-access',navigation:{label:'Access & Sign-in',description:'Platform access address and sign-in links',icon:Globe,order:100,group:'settings'},component:AccessLoginPage,isVisible:({currentUser})=>currentUser.systemAdmin===true},
{id:'signin-security',view:'extension:signin-security',navigation:{label:'Sign-in & Security',description:'Password and Google sign-in',icon:KeyRound,order:20,group:'settings'},component:AccountAccess},
{id:'organization',view:'extension:organization',navigation:{label:'Organization',description:'Your organization information',icon:Building2,order:30,group:'settings'},component:OrganizationInformation},
{id:'members',view:'extension:members',navigation:{label:'Members',description:'Organization members and invitations',icon:Users,order:40,group:'settings'},component:MembersPage},{id:'community-migration',view:'extension:community-migration',navigation:{label:'Upgrade to Enterprise',description:'Authorize and monitor Community migration',icon:Building2,order:130,group:'settings'},component:({toast})=><CommunityMigrationAuthorizationsPage toast={toast}/>,isVisible:({currentUser,capabilities})=>currentUser.systemAdmin===true&&capabilities.advancedTenancy?.enabled!==true}];
createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    {['/signup','/join','/auth/complete'].includes(window.location.pathname)?<PublicAuthPage/>:<App modules={modules} profileComponent={PersonalInformation} profileLabel="Personal Information" loginSupplement={<SaaSLoginSupplement/>}/>}
  </React.StrictMode>
);
