create type votes.state as enum ('new', 'text', 'variants', 'open', 'close');

create table if not exists votes.t_poll
(
    id        uuid        default gen_random_uuid() not null
        constraint pk_id
            primary key,
    author_id bigint                                not null,
    question  text,
    variants  text[],
    state     votes.state                           not null,
    create_dt timestamptz default now()             not null,
    update_dt timestamptz default now()             not null
);

alter table votes.t_poll
    alter column state set default 'new';

create index idx_author_id
    on votes.t_poll (author_id);

create index idx_create_dt
    on votes.t_poll (create_dt);

create index idx_user_poss
    on votes.t_poll (author_id, create_dt desc);


create function poll_update_dt() returns trigger
    language plpgsql
as
$$
BEGIN

    -- DON'T DELETE
    NEW.update_dt = now();

    RETURN NEW;
END;
$$;

alter function poll_update_dt() owner to bot;

CREATE TRIGGER poll_update_dt
    BEFORE UPDATE
    ON votes.t_poll
    FOR EACH ROW
EXECUTE PROCEDURE poll_update_dt();

create table if not exists votes.t_vote
(
    id       bigserial primary key,
    poll_id  uuid references votes.t_poll not null,
    user_id  bigint                       not null,
    variant  int                          not null,
    username text                         not null
);

