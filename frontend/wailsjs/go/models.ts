export namespace app {
	
	export class ConnectHostResult {
	    ok: boolean;
	    sessionId: string;
	    errorCode: string;
	    fingerprint: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectHostResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.sessionId = source["sessionId"];
	        this.errorCode = source["errorCode"];
	        this.fingerprint = source["fingerprint"];
	        this.message = source["message"];
	    }
	}
	export class HostInput {
	    id: string;
	    groupId: string;
	    name: string;
	    addr: string;
	    port: number;
	    username: string;
	    authType: string;
	    keyPath: string;
	    hasSecret: boolean;
	    initCommands: string[];
	    keepaliveSec: number;
	    autoReconnect: boolean;
	    // Go type: time
	    lastConnectedAt: any;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    password: string;
	    passphrase: string;
	
	    static createFrom(source: any = {}) {
	        return new HostInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.groupId = source["groupId"];
	        this.name = source["name"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.keyPath = source["keyPath"];
	        this.hasSecret = source["hasSecret"];
	        this.initCommands = source["initCommands"];
	        this.keepaliveSec = source["keepaliveSec"];
	        this.autoReconnect = source["autoReconnect"];
	        this.lastConnectedAt = this.convertValues(source["lastConnectedAt"], null);
	        this.sort = source["sort"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.password = source["password"];
	        this.passphrase = source["passphrase"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace store {
	
	export class Group {
	    id: string;
	    name: string;
	    parentId: string;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.parentId = source["parentId"];
	        this.sort = source["sort"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Host {
	    id: string;
	    groupId: string;
	    name: string;
	    addr: string;
	    port: number;
	    username: string;
	    authType: string;
	    keyPath: string;
	    hasSecret: boolean;
	    initCommands: string[];
	    keepaliveSec: number;
	    autoReconnect: boolean;
	    // Go type: time
	    lastConnectedAt: any;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Host(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.groupId = source["groupId"];
	        this.name = source["name"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.keyPath = source["keyPath"];
	        this.hasSecret = source["hasSecret"];
	        this.initCommands = source["initCommands"];
	        this.keepaliveSec = source["keepaliveSec"];
	        this.autoReconnect = source["autoReconnect"];
	        this.lastConnectedAt = this.convertValues(source["lastConnectedAt"], null);
	        this.sort = source["sort"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snippet {
	    id: string;
	    name: string;
	    content: string;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Snippet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.sort = source["sort"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sftp {
	
	export class RemoteEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    mode: string;
	    // Go type: time
	    mtime: any;
	
	    static createFrom(source: any = {}) {
	        return new RemoteEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.mode = source["mode"];
	        this.mtime = this.convertValues(source["mtime"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Task {
	    transferId: string;
	    batchId: string;
	    sessionId: string;
	    direction: string;
	    name: string;
	    remotePath: string;
	    localPath: string;
	    size: number;
	    transferred: number;
	    state: string;
	    speedBps: number;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.transferId = source["transferId"];
	        this.batchId = source["batchId"];
	        this.sessionId = source["sessionId"];
	        this.direction = source["direction"];
	        this.name = source["name"];
	        this.remotePath = source["remotePath"];
	        this.localPath = source["localPath"];
	        this.size = source["size"];
	        this.transferred = source["transferred"];
	        this.state = source["state"];
	        this.speedBps = source["speedBps"];
	        this.err = source["err"];
	    }
	}

}
