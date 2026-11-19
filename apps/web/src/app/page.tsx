import { PostCard, type Post } from "@/components/PostCard";
import { SiteHeader } from "@/components/SiteHeader";
import { getHealth } from "@/lib/api";

// 示例数据：M1 阶段用于验证界面与数据形状，接入真实 API 后移除。
const FEED: Post[] = [
  {
    id: "p1",
    author: {
      name: "林正则",
      handle: "linzz",
      title: "教授 · 计算机视觉",
      verified: true,
      influencer: true,
      initial: "林",
    },
    timeAgo: "2 小时前",
    content:
      "这篇把数据筛选讲透了，做指令微调的同学值得精读。图 3 的消融实验尤其关键，说明数据配比的影响远大于模型规模。",
    paper: {
      doi: "arXiv:2405.13412",
      title: "Towards Robust Instruction Tuning via Data Curation",
      authors: "L. Chen, W. Zhang, Y. Liu, et al.",
      abstract:
        "Instruction tuning is highly sensitive to the composition of its training data. We show that a small, carefully curated subset can match or exceed the performance of a much larger, unfiltered dataset, and we characterize which properties of the data drive this effect.",
      pageCount: 12,
      currentPage: 1,
    },
    stats: { likes: 214, comments: 36, reposts: 89 },
    tags: ["机器学习", "大语言模型", "指令微调"],
  },
  {
    id: "p2",
    author: {
      name: "周砚",
      handle: "zhouyan",
      title: "博士候选人 · 自然语言处理",
      verified: true,
      influencer: false,
      initial: "周",
    },
    timeAgo: "5 小时前",
    content:
      "关于长上下文检索的综述，整理得很系统。个人觉得第 4 节的评测方法部分值得单独拿出来讨论，现有 benchmark 的覆盖度确实不够。",
    paper: {
      doi: "arXiv:2404.09871",
      title: "A Survey on Long-Context Retrieval for Language Models",
      authors: "Y. Zhou, K. Wang, et al.",
      abstract:
        "We survey retrieval mechanisms designed for long-context language models, comparing sparse, dense, and hybrid approaches along the axes of recall, latency, and training cost.",
      pageCount: 28,
      currentPage: 1,
    },
    stats: { likes: 96, comments: 18, reposts: 31 },
    tags: ["检索增强", "长上下文"],
  },
];

export default async function HomePage() {
  const health = await getHealth();

  return (
    <>
      <SiteHeader />

      <main className="mx-auto max-w-6xl px-5 py-10">
        {/* 页面标题区 */}
        <div className="mb-8 border-b border-line pb-7">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            关注流 · Following
          </p>
          <h1 className="font-display text-[clamp(1.8rem,4vw,2.6rem)] leading-[1.05] font-semibold tracking-[-0.03em] text-balance">
            以人的判断，筛选前沿论文
          </h1>
          <p className="mt-3 max-w-[62ch] text-[1rem] text-ink-2">
            不做个性化推荐算法。你看到的内容，来自你关注的人的选择。
          </p>
        </div>

        <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_280px]">
          {/* 信息流 */}
          <div className="flex flex-col gap-4">
            {/* 三档流切换 */}
            <div className="flex items-center gap-1.5">
              <Tab active>关注流</Tab>
              <Tab>全站流</Tab>
              <Tab>热门流</Tab>
              <span className="ml-2 font-mono text-[0.7rem] text-ink-3">时间倒序 · 无加权</span>
            </div>

            {FEED.map((post) => (
              <PostCard key={post.id} post={post} />
            ))}
          </div>

          {/* 侧栏 */}
          <aside className="hidden flex-col gap-4 lg:flex">
            <SidePanel title="排序规则">
              <p className="text-[0.82rem] leading-relaxed text-ink-2">
                关注流按发布时间倒序，不做任何加权。热门流使用公开的热度公式，规则对所有人一致。
              </p>
              <pre className="mt-3 overflow-x-auto rounded-[6px] border border-line bg-surface-2 p-3 font-mono text-[0.72rem] leading-relaxed text-ink-2">
{`raw = 1.0·likes + 2.0·comments
    + 3.0·reposts
hot = raw / (hours + 2)^1.5`}
              </pre>
            </SidePanel>

            <SidePanel title="服务状态">
              <div className="flex flex-col gap-2 text-[0.82rem]">
                <StatusRow label="API" ok={health?.status === "ok"} value={health?.status ?? "不可达"} />
                <StatusRow label="数据库" ok={health?.database === "up"} value={health?.database ?? "—"} />
                <StatusRow label="版本" ok value={health?.version ?? "—"} mono />
              </div>
            </SidePanel>
          </aside>
        </div>
      </main>
    </>
  );
}

function Tab({ children, active = false }: { children: React.ReactNode; active?: boolean }) {
  return (
    <span
      className={`cursor-default rounded-[6px] px-3.5 py-1.5 text-[0.9rem] ${
        active
          ? "border border-line bg-surface font-semibold text-ink"
          : "text-ink-2 hover:bg-surface-2"
      }`}
    >
      {children}
    </span>
  );
}

function SidePanel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-[12px] border border-line bg-surface p-4">
      <h2 className="mb-3 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
        {title}
      </h2>
      {children}
    </section>
  );
}

function StatusRow({
  label,
  ok,
  value,
  mono = false,
}: {
  label: string;
  ok: boolean;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-ink-2">{label}</span>
      <span className={`flex items-center gap-1.5 ${mono ? "font-mono" : ""}`}>
        <span className={`size-1.5 rounded-full ${ok ? "bg-good" : "bg-danger"}`} />
        {value}
      </span>
    </div>
  );
}
