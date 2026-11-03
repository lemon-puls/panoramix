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
	export class DBConnInput {
	    id: string;
	    name: string;
	    kind: string;
	    addr: string;
	    port: number;
	    username: string;
	    dbName: string;
	    hasSecret: boolean;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    password: string;
	
	    static createFrom(source: any = {}) {
	        return new DBConnInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.dbName = source["dbName"];
	        this.hasSecret = source["hasSecret"];
	        this.sort = source["sort"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.password = source["password"];
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
	    loginScript: store.LoginStep[];
	    proxyChain: string[];
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
	        this.loginScript = this.convertValues(source["loginScript"], store.LoginStep);
	        this.proxyChain = source["proxyChain"];
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
	export class ImportPreview {
	    source: string;
	    file: string;
	    name: string;
	    host: string;
	    port: number;
	    username: string;
	    authType: string;
	    keyName?: string;
	    hasProxy?: boolean;
	    valid: boolean;
	    skipReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.file = source["file"];
	        this.name = source["name"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.keyName = source["keyName"];
	        this.hasProxy = source["hasProxy"];
	        this.valid = source["valid"];
	        this.skipReason = source["skipReason"];
	    }
	}

}

export namespace db {
	
	export class KeyInfo {
	    key: string;
	    ttl: number;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new KeyInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.ttl = source["ttl"];
	        this.type = source["type"];
	    }
	}
	export class KeyPage {
	    keys: KeyInfo[];
	    cursor: number;
	
	    static createFrom(source: any = {}) {
	        return new KeyPage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keys = this.convertValues(source["keys"], KeyInfo);
	        this.cursor = source["cursor"];
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
	export class Result {
	    columns: string[];
	    rows: string[][];
	    affected: number;
	    durationMs: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.affected = source["affected"];
	        this.durationMs = source["durationMs"];
	        this.truncated = source["truncated"];
	    }
	}
	export class ZMember {
	    member: string;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new ZMember(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.member = source["member"];
	        this.score = source["score"];
	    }
	}
	export class Value {
	    key: string;
	    type: string;
	    ttl: number;
	    str?: string;
	    hash?: Record<string, string>;
	    list?: string[];
	    set?: string[];
	    zset?: ZMember[];
	    total: number;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Value(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.ttl = source["ttl"];
	        this.str = source["str"];
	        this.hash = source["hash"];
	        this.list = source["list"];
	        this.set = source["set"];
	        this.zset = this.convertValues(source["zset"], ZMember);
	        this.total = source["total"];
	        this.truncated = source["truncated"];
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

export namespace fsx {
	
	export class Entry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    // Go type: time
	    mtime: any;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
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

}

export namespace sftp {
	
	export class EditInfo {
	    id: string;
	    sessionId: string;
	    remotePath: string;
	    localPath: string;
	    name: string;
	    conflict: boolean;
	    uploadedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new EditInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.remotePath = source["remotePath"];
	        this.localPath = source["localPath"];
	        this.name = source["name"];
	        this.conflict = source["conflict"];
	        this.uploadedAt = source["uploadedAt"];
	    }
	}
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
	    resumed?: boolean;
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
	        this.resumed = source["resumed"];
	        this.err = source["err"];
	    }
	}

}

export namespace store {
	
	export class DBConn {
	    id: string;
	    name: string;
	    kind: string;
	    addr: string;
	    port: number;
	    username: string;
	    dbName: string;
	    hasSecret: boolean;
	    sort: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new DBConn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.addr = source["addr"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.dbName = source["dbName"];
	        this.hasSecret = source["hasSecret"];
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
	export class LoginStep {
	    expect: string;
	    send: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.expect = source["expect"];
	        this.send = source["send"];
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
	    loginScript: LoginStep[];
	    proxyChain: string[];
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
	        this.loginScript = this.convertValues(source["loginScript"], LoginStep);
	        this.proxyChain = source["proxyChain"];
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

