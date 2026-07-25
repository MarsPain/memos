import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { LoaderIcon, SearchIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useMemoSearch } from "@/hooks/useAIQueries";
import {
  type MemoSearchResult,
  SearchMemosFilterSchema,
  type SearchMemosRequest,
  SearchMemosRequestSchema,
} from "@/types/proto/api/v1/ai_service_pb";
import { Visibility } from "@/types/proto/api/v1/memo_service_pb";
import { useTranslate } from "@/utils/i18n";

// memoUid extracts the UID from a memo resource name of the form
// memos/{memo}.
const memoUid = (name: string) => name.split("/").pop() ?? name;

// parseDateBoundary parses a yyyy-mm-dd date input into a proto Timestamp at
// a day boundary: the start of the day for the after bound and the start of
// the next day for the before bound, so both bounds are inclusive by day.
const parseDateBoundary = (value: string, endOfDay: boolean) => {
  if (!value) {
    return undefined;
  }
  const date = new Date(`${value}T00:00:00`);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  if (endOfDay) {
    date.setDate(date.getDate() + 1);
  }
  return timestampFromDate(date);
};

const SearchResultItem = ({ result }: { result: MemoSearchResult }) => (
  <div className="flex flex-col gap-1 rounded-lg border border-border px-4 py-3">
    <Link to={`/memos/${memoUid(result.memo)}`} viewTransition className="text-sm font-medium text-primary hover:underline">
      {result.memo}
    </Link>
    {result.snippet !== "" && <p className="text-sm whitespace-pre-wrap break-words text-muted-foreground">{result.snippet}</p>}
    {result.rankReasons.length > 0 && (
      <div className="flex flex-wrap gap-1">
        {result.rankReasons.map((reason) => (
          <Badge key={reason} variant="outline" className="text-xs">
            {reason}
          </Badge>
        ))}
      </div>
    )}
  </div>
);

const Search = () => {
  const t = useTranslate();
  const [query, setQuery] = useState("");
  const [tags, setTags] = useState("");
  const [visibility, setVisibility] = useState("any");
  const [createdAfter, setCreatedAfter] = useState("");
  const [createdBefore, setCreatedBefore] = useState("");
  const [creator, setCreator] = useState("");
  // The request last submitted; typing only edits the pending inputs.
  const [submitted, setSubmitted] = useState<SearchMemosRequest | undefined>(undefined);

  const searchQuery = useMemoSearch(submitted);
  const results = searchQuery.data?.results ?? [];
  const partialReasons = searchQuery.data?.partialReasons ?? [];

  const handleSearch = () => {
    const trimmed = query.trim();
    if (trimmed === "") {
      return;
    }
    setSubmitted(
      create(SearchMemosRequestSchema, {
        query: trimmed,
        filter: create(SearchMemosFilterSchema, {
          tags: tags
            .split(/[\s,]+/)
            .map((tag) => tag.trim().replace(/^#/, ""))
            .filter((tag) => tag !== ""),
          visibility: visibility === "any" ? Visibility.VISIBILITY_UNSPECIFIED : (Number(visibility) as Visibility),
          createdAfter: parseDateBoundary(createdAfter, false),
          createdBefore: parseDateBoundary(createdBefore, true),
          creator: creator.trim(),
        }),
      }),
    );
  };

  return (
    <section className="mx-auto flex w-full max-w-3xl flex-col gap-4 pb-10">
      <div className="flex items-center gap-2 border-b border-border pb-4">
        <SearchIcon className="h-5 w-5 text-muted-foreground" />
        <h1 className="text-2xl font-semibold tracking-normal text-foreground">{t("search.title")}</h1>
      </div>

      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          handleSearch();
        }}
      >
        <div className="flex flex-row items-center gap-2">
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("search.input-placeholder")}
            aria-label={t("search.input-placeholder")}
            className="flex-1"
          />
          <Button type="submit" disabled={query.trim() === "" || searchQuery.isFetching}>
            {searchQuery.isFetching ? <LoaderIcon className="h-4 w-4 animate-spin" /> : <SearchIcon className="h-4 w-4" />}
            {t("search.run")}
          </Button>
        </div>

        <details className="rounded-lg border border-border px-4 py-2">
          <summary className="cursor-pointer text-sm font-medium text-muted-foreground">{t("search.filters-title")}</summary>
          <div className="grid grid-cols-1 gap-3 pt-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <Label htmlFor="search-tags">{t("search.filter-tags")}</Label>
              <Input
                id="search-tags"
                value={tags}
                onChange={(event) => setTags(event.target.value)}
                placeholder={t("search.filter-tags-placeholder")}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label>{t("search.filter-visibility")}</Label>
              <Select value={visibility} onValueChange={setVisibility}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="any">{t("search.filter-visibility-any")}</SelectItem>
                  <SelectItem value={String(Visibility.PRIVATE)}>{t("memo.visibility.private")}</SelectItem>
                  <SelectItem value={String(Visibility.PROTECTED)}>{t("memo.visibility.protected")}</SelectItem>
                  <SelectItem value={String(Visibility.PUBLIC)}>{t("memo.visibility.public")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="search-created-after">{t("search.filter-created-after")}</Label>
              <Input id="search-created-after" type="date" value={createdAfter} onChange={(event) => setCreatedAfter(event.target.value)} />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="search-created-before">{t("search.filter-created-before")}</Label>
              <Input
                id="search-created-before"
                type="date"
                value={createdBefore}
                onChange={(event) => setCreatedBefore(event.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="search-creator">{t("search.filter-creator")}</Label>
              <Input
                id="search-creator"
                value={creator}
                onChange={(event) => setCreator(event.target.value)}
                placeholder={t("search.filter-creator-placeholder")}
              />
            </div>
          </div>
        </details>
      </form>

      {partialReasons.length > 0 && (
        <div role="alert" className="rounded-lg border border-warning/40 bg-warning/10 px-4 py-2 text-sm text-foreground">
          {t("search.partial-banner", { reasons: partialReasons.join(", ") })}
        </div>
      )}

      {searchQuery.isPending && submitted ? (
        <div className="flex justify-center py-10">
          <LoaderIcon className="h-6 w-6 animate-spin text-muted-foreground" />
        </div>
      ) : searchQuery.isError ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("search.load-failed")}</p>
        </div>
      ) : !submitted ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("search.empty")}</p>
        </div>
      ) : results.length === 0 && !searchQuery.isFetching ? (
        <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center">
          <p className="text-sm text-muted-foreground">{t("search.no-results")}</p>
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {results.map((result) => (
            <SearchResultItem key={result.memo} result={result} />
          ))}
        </div>
      )}
    </section>
  );
};

export default Search;
