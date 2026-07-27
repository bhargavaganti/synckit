export namespace main {
	
	export class KeyStatus {
	    enabled: boolean;
	    recipient: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new KeyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.recipient = source["recipient"];
	        this.path = source["path"];
	    }
	}
	export class SnapshotResult {
	    bundle: string;
	    instances: number;
	    skipped: string[];
	    encrypted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SnapshotResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bundle = source["bundle"];
	        this.instances = source["instances"];
	        this.skipped = source["skipped"];
	        this.encrypted = source["encrypted"];
	    }
	}

}

export namespace restore {
	
	export class AppOutcome {
	    app: string;
	    instance: string;
	    target: string;
	    backup: string;
	    restored: boolean;
	    warnings: string[];
	    skipped: string;
	
	    static createFrom(source: any = {}) {
	        return new AppOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.instance = source["instance"];
	        this.target = source["target"];
	        this.backup = source["backup"];
	        this.restored = source["restored"];
	        this.warnings = source["warnings"];
	        this.skipped = source["skipped"];
	    }
	}

}

export namespace service {
	
	export class Instance {
	    id: string;
	    label: string;
	    version: string;
	    root: string;
	    running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Instance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.version = source["version"];
	        this.root = source["root"];
	        this.running = source["running"];
	    }
	}
	export class App {
	    id: string;
	    installed: boolean;
	    secretsCrossMachine: boolean;
	    note: string;
	    instances: Instance[];
	
	    static createFrom(source: any = {}) {
	        return new App(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.installed = source["installed"];
	        this.secretsCrossMachine = source["secretsCrossMachine"];
	        this.note = source["note"];
	        this.instances = this.convertValues(source["instances"], Instance);
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
	export class Bundle {
	    name: string;
	    id: string;
	    createdAt: string;
	    // Go type: time
	    createdTime: any;
	    apps: string[];
	    sizeMB: number;
	    originOS: string;
	    originHost: string;
	
	    static createFrom(source: any = {}) {
	        return new Bundle(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.id = source["id"];
	        this.createdAt = source["createdAt"];
	        this.createdTime = this.convertValues(source["createdTime"], null);
	        this.apps = source["apps"];
	        this.sizeMB = source["sizeMB"];
	        this.originOS = source["originOS"];
	        this.originHost = source["originHost"];
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
	export class Cell {
	    present: boolean;
	    version: string;
	    profileId: string;
	    running: boolean;
	    fingerprint?: string;
	    // Go type: time
	    snapshotAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new Cell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.present = source["present"];
	        this.version = source["version"];
	        this.profileId = source["profileId"];
	        this.running = source["running"];
	        this.fingerprint = source["fingerprint"];
	        this.snapshotAt = this.convertValues(source["snapshotAt"], null);
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
	
	export class Machine {
	    hostname: string;
	    os: string;
	    arch: string;
	    user: string;
	
	    static createFrom(source: any = {}) {
	        return new Machine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hostname = source["hostname"];
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.user = source["user"];
	    }
	}
	export class MatrixRow {
	    app: string;
	    role: string;
	    secretsCrossMachine: boolean;
	    cells: Record<string, Cell>;
	    verdict: string;
	    note: string;
	    sync: string;
	    newestHost?: string;
	
	    static createFrom(source: any = {}) {
	        return new MatrixRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.role = source["role"];
	        this.secretsCrossMachine = source["secretsCrossMachine"];
	        this.cells = this.convertValues(source["cells"], Cell, true);
	        this.verdict = source["verdict"];
	        this.note = source["note"];
	        this.sync = source["sync"];
	        this.newestHost = source["newestHost"];
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
	export class Matrix {
	    machines: string[];
	    rows: MatrixRow[];
	
	    static createFrom(source: any = {}) {
	        return new Matrix(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.machines = source["machines"];
	        this.rows = this.convertValues(source["rows"], MatrixRow);
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
	
	export class Peer {
	    host: string;
	    ip: string;
	    os: string;
	    online: boolean;
	    serving: boolean;
	    bundles: Bundle[];
	
	    static createFrom(source: any = {}) {
	        return new Peer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.ip = source["ip"];
	        this.os = source["os"];
	        this.online = source["online"];
	        this.serving = source["serving"];
	        this.bundles = this.convertValues(source["bundles"], Bundle);
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
	export class Overview {
	    machine: Machine;
	    apps: App[];
	    localBundles: Bundle[];
	    peers: Peer[];
	    tailscaleUp: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.machine = this.convertValues(source["machine"], Machine);
	        this.apps = this.convertValues(source["apps"], App);
	        this.localBundles = this.convertValues(source["localBundles"], Bundle);
	        this.peers = this.convertValues(source["peers"], Peer);
	        this.tailscaleUp = source["tailscaleUp"];
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

export namespace settings {
	
	export class Settings {
	    tailscalePath?: string;
	    ignore?: Record<string, Array<string>>;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tailscalePath = source["tailscalePath"];
	        this.ignore = source["ignore"];
	    }
	}

}

