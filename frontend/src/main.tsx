import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import type { HyperCDRFrontendModule } from "./app/extensions";
import { Building2 } from 'lucide-react';
import CommunityMigrationAuthorizationsPage from './features/migrations/community-migration-authorizations-page';
import "./styles.css";

const modules:HyperCDRFrontendModule[]=[{id:'community-migration',view:'extension:community-migration',navigation:{label:'Upgrade to Enterprise',description:'Authorize and monitor Community migration',icon:Building2,order:130,group:'settings'},component:({toast})=><CommunityMigrationAuthorizationsPage toast={toast}/>,isVisible:({currentUser,capabilities})=>currentUser.systemAdmin===true&&capabilities.advancedTenancy?.enabled!==true}];
createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    <App modules={modules} />
  </React.StrictMode>
);
