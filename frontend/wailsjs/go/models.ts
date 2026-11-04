export namespace ai {
	
	export class Config {
	    enabled: boolean;
	    provider: string;
	    baseUrl: string;
	    model: string;
	    temperature: number;
	    timeoutSec: number;
	    allowWrite: boolean;
	    allowInsecureTls: boolean;
	    concurrency: number;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.timeoutSec = source["timeoutSec"];
	        this.allowWrite = source["allowWrite"];
	        this.allowInsecureTls = source["allowInsecureTls"];
	        this.concurrency = source["concurrency"];
	    }
	}
	export class Warning {
	    code: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new Warning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.detail = source["detail"];
	    }
	}
	export class ConfigView {
	    enabled: boolean;
	    provider: string;
	    baseUrl: string;
	    model: string;
	    temperature: number;
	    timeoutSec: number;
	    allowWrite: boolean;
	    allowInsecureTls: boolean;
	    concurrency: number;
	    hasKey: boolean;
	    endpoint: string;
	    endpointHash: string;
	    scope: string;
	    cloudConsent: boolean;
	    warnings: Warning[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.provider = source["provider"];
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.temperature = source["temperature"];
	        this.timeoutSec = source["timeoutSec"];
	        this.allowWrite = source["allowWrite"];
	        this.allowInsecureTls = source["allowInsecureTls"];
	        this.concurrency = source["concurrency"];
	        this.hasKey = source["hasKey"];
	        this.endpoint = source["endpoint"];
	        this.endpointHash = source["endpointHash"];
	        this.scope = source["scope"];
	        this.cloudConsent = source["cloudConsent"];
	        this.warnings = this.convertValues(source["warnings"], Warning);
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
	export class SensitiveHint {
	    field: string;
	    kind: string;

	    static createFrom(source: any = {}) {
	        return new SensitiveHint(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.kind = source["kind"];
	    }
	}
	export class TablePreview {
	    name: string;
	    chars: number;
	    excluded: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new TablePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.chars = source["chars"];
	        this.excluded = source["excluded"];
	        this.reason = source["reason"];
	    }
	}
	export class Prepared {
	    prepareId: string;
	    requestId: string;
	    payload: string;
	    payloadHash: string;
	    promptChars: number;
	    schemaChars: number;
	    tables: TablePreview[];
	    warnings: Warning[];
	    sensitiveHints: SensitiveHint[];
	    endpoint: string;
	    endpointHash: string;
	    scope: string;
	    provider: string;
	    model: string;
	    needsConsent: boolean;
	    expiresInSec: number;
	    allowWrite: boolean;
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new Prepared(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prepareId = source["prepareId"];
	        this.requestId = source["requestId"];
	        this.payload = source["payload"];
	        this.payloadHash = source["payloadHash"];
	        this.promptChars = source["promptChars"];
	        this.schemaChars = source["schemaChars"];
	        this.tables = this.convertValues(source["tables"], TablePreview);
	        this.warnings = this.convertValues(source["warnings"], Warning);
	        this.sensitiveHints = this.convertValues(source["sensitiveHints"], SensitiveHint);
	        this.endpoint = source["endpoint"];
	        this.endpointHash = source["endpointHash"];
	        this.scope = source["scope"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.needsConsent = source["needsConsent"];
	        this.expiresInSec = source["expiresInSec"];
	        this.allowWrite = source["allowWrite"];
	        this.mode = source["mode"];
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
	export class Request {
	    requestId: string;
	    connId: string;
	    database: string;
	    mode: string;
	    question: string;
	    tables: string[];
	    sql: string;
	    error: string;
	    locale: string;
	
	    static createFrom(source: any = {}) {
	        return new Request(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.connId = source["connId"];
	        this.database = source["database"];
	        this.mode = source["mode"];
	        this.question = source["question"];
	        this.tables = source["tables"];
	        this.sql = source["sql"];
	        this.error = source["error"];
	        this.locale = source["locale"];
	    }
	}
	export class Result {
	    sql: string;
	    explanation: string;
	    warnings: Warning[];
	    provider: string;
	    model: string;
	    durationMs: number;
	    promptChars: number;
	    responseChars: number;
	    isWrite: boolean;
	    multiStatement: boolean;
	    dangerous: boolean;
	    uncertain: boolean;
	    insertAllowed: boolean;
	    insertBlockReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.explanation = source["explanation"];
	        this.warnings = this.convertValues(source["warnings"], Warning);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.durationMs = source["durationMs"];
	        this.promptChars = source["promptChars"];
	        this.responseChars = source["responseChars"];
	        this.isWrite = source["isWrite"];
	        this.multiStatement = source["multiStatement"];
	        this.dangerous = source["dangerous"];
	        this.uncertain = source["uncertain"];
	        this.insertAllowed = source["insertAllowed"];
	        this.insertBlockReason = source["insertBlockReason"];
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
	
	export class ColumnInfo {
	    field: string;
	    type: string;
	    null: string;
	    key: string;
	    default?: string;
	    extra: string;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new ColumnInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.type = source["type"];
	        this.null = source["null"];
	        this.key = source["key"];
	        this.default = source["default"];
	        this.extra = source["extra"];
	        this.comment = source["comment"];
	    }
	}
	export class ForeignKeyInfo {
	    name: string;
	    column: string;
	    refTable: string;
	    refColumn: string;
	    onUpdate: string;
	    onDelete: string;
	
	    static createFrom(source: any = {}) {
	        return new ForeignKeyInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.column = source["column"];
	        this.refTable = source["refTable"];
	        this.refColumn = source["refColumn"];
	        this.onUpdate = source["onUpdate"];
	        this.onDelete = source["onDelete"];
	    }
	}
	export class IndexInfo {
	    name: string;
	    type: string;
	    unique: boolean;
	    columns: string[];
	
	    static createFrom(source: any = {}) {
	        return new IndexInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.unique = source["unique"];
	        this.columns = source["columns"];
	    }
	}
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
	export class TableInfo {
	    name: string;
	    isView: boolean;
	    rows: number;
	    comment: string;
	
	    static createFrom(source: any = {}) {
	        return new TableInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.isView = source["isView"];
	        this.rows = source["rows"];
	        this.comment = source["comment"];
	    }
	}
	export class TableMeta {
	    name: string;
	    engine: string;
	    collation: string;
	    comment: string;
	    createTime: string;
	    rows: number;
	    dataLength: number;
	    indexLength: number;
	    autoIncrement: number;
	
	    static createFrom(source: any = {}) {
	        return new TableMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.engine = source["engine"];
	        this.collation = source["collation"];
	        this.comment = source["comment"];
	        this.createTime = source["createTime"];
	        this.rows = source["rows"];
	        this.dataLength = source["dataLength"];
	        this.indexLength = source["indexLength"];
	        this.autoIncrement = source["autoIncrement"];
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

